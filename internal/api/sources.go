package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/production"
	"github.com/jackc/pgx/v5"
)

type ProjectSource struct {
	ID               string          `json:"id"`
	OrganizationID   string          `json:"organizationId"`
	ProjectID        string          `json:"projectId"`
	SourceType       string          `json:"sourceType"`
	Title            string          `json:"title"`
	Content          string          `json:"content,omitempty"`
	ContentFormat    string          `json:"contentFormat"`
	OriginalFileName *string         `json:"originalFileName,omitempty"`
	StorageKey       *string         `json:"storageKey,omitempty"`
	Status           string          `json:"status"`
	Metadata         json.RawMessage `json:"metadata"`
	Revision         int64           `json:"revision"`
	ContentRevision  int64           `json:"contentRevision"`
	ContentHash      string          `json:"contentHash"`
	ChapterCount     int             `json:"chapterCount,omitempty"`
	FirstVolumeIndex int             `json:"firstVolumeIndex,omitempty"`
	CreatedBy        *string         `json:"createdBy,omitempty"`
	CreatedAt        time.Time       `json:"createdAt"`
	UpdatedAt        time.Time       `json:"updatedAt"`
	Chapters         []NovelChapter  `json:"chapters,omitempty"`
}

type NovelChapter struct {
	ID           string          `json:"id"`
	SourceID     string          `json:"sourceId"`
	ChapterIndex int             `json:"chapterIndex"`
	VolumeIndex  *int            `json:"volumeIndex,omitempty"`
	SectionIndex *int            `json:"sectionIndex,omitempty"`
	VolumeTitle  *string         `json:"volumeTitle,omitempty"`
	ChapterTitle *string         `json:"chapterTitle,omitempty"`
	Content      string          `json:"content"`
	EventState   string          `json:"eventState"`
	EventSummary json.RawMessage `json:"eventSummary,omitempty"`
	ErrorMessage *string         `json:"errorMessage,omitempty"`
	CreatedAt    time.Time       `json:"createdAt"`
	UpdatedAt    time.Time       `json:"updatedAt"`
}

type NovelChapterSummary struct {
	ID                      string          `json:"id"`
	SourceID                string          `json:"sourceId"`
	ChapterIndex            int             `json:"chapterIndex"`
	VolumeIndex             *int            `json:"volumeIndex,omitempty"`
	SectionIndex            *int            `json:"sectionIndex,omitempty"`
	VolumeTitle             *string         `json:"volumeTitle,omitempty"`
	ChapterTitle            *string         `json:"chapterTitle,omitempty"`
	ContentLength           int             `json:"contentLength"`
	EventState              string          `json:"eventState"`
	EventSummary            json.RawMessage `json:"eventSummary,omitempty"`
	ErrorMessage            *string         `json:"errorMessage,omitempty"`
	EventCount              int             `json:"eventCount"`
	ApprovedEventCount      int             `json:"approvedEventCount"`
	PendingEventReviewCount int             `json:"pendingEventReviewCount"`
	CreatedAt               time.Time       `json:"createdAt"`
	UpdatedAt               time.Time       `json:"updatedAt"`
}

type DeleteSourceChapterResponse struct {
	Deleted               bool   `json:"deleted"`
	Mode                  string `json:"mode"`
	SourceID              string `json:"sourceId"`
	ChapterID             string `json:"chapterId"`
	DeletedChapterIndex   int    `json:"deletedChapterIndex"`
	RemainingChapterCount int    `json:"remainingChapterCount"`
}

type CreatedScriptSummary struct {
	ID               string `json:"id"`
	CurrentVersionID string `json:"currentVersionId"`
	Title            string `json:"title"`
}

type ImportProjectSourceResponse struct {
	Source   ProjectSource         `json:"source"`
	Chapters []NovelChapterSummary `json:"chapters"`
	Script   *CreatedScriptSummary `json:"script,omitempty"`
}

type OutputImpactAffected struct {
	EntityType string `json:"entityType"`
	Count      int    `json:"count"`
}

type OutputImpact struct {
	EntityType      string                 `json:"entityType"`
	EntityID        string                 `json:"entityId"`
	CanDelete       bool                   `json:"canDelete"`
	RecommendedMode string                 `json:"recommendedMode"`
	DeleteModes     []string               `json:"deleteModes"`
	Affected        []OutputImpactAffected `json:"affected"`
	Warnings        []string               `json:"warnings"`
}

type sourceChapterRequest struct {
	ID           *string `json:"id"`
	ChapterIndex *int    `json:"chapterIndex"`
	VolumeIndex  *int    `json:"volumeIndex"`
	SectionIndex *int    `json:"sectionIndex"`
	VolumeTitle  *string `json:"volumeTitle"`
	ChapterTitle *string `json:"chapterTitle"`
	Content      string  `json:"content"`
}

type sourceChapterIdentityRecord struct {
	ID           string
	ChapterIndex int
	VolumeIndex  sql.NullInt32
	SectionIndex sql.NullInt32
	VolumeTitle  sql.NullString
	ChapterTitle sql.NullString
	Content      string
}

type importProjectSourceRequest struct {
	SourceType       string                 `json:"sourceType"`
	Title            string                 `json:"title"`
	Content          string                 `json:"content"`
	ContentFormat    string                 `json:"contentFormat"`
	OriginalFileName *string                `json:"originalFileName"`
	StorageKey       *string                `json:"storageKey"`
	Metadata         json.RawMessage        `json:"metadata"`
	Chapters         []sourceChapterRequest `json:"chapters"`
	SplitChapters    *bool                  `json:"splitChapters"`
	CreateScript     *bool                  `json:"createScript"`
	IdempotencyKey   string                 `json:"idempotencyKey,omitempty"`
	ImportMethod     string                 `json:"-"`
	FileName         string                 `json:"-"`
	FileSize         int64                  `json:"-"`
}

func (s *Server) listProjectSources(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionSourceRead)
	if !ok {
		return
	}
	statusFilter, valid := parseArchivedStatusFilter(r.URL.Query().Get("filter[status]"))
	if !valid {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "source status filter is invalid", nil, false)
		return
	}
	items, err := s.projectSourceList(r, project.ID, statusFilter)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) projectSourceList(r *http.Request, projectID, statusFilter string) ([]ProjectSource, error) {
	return s.projectSourceListContext(r.Context(), projectID, statusFilter)
}

func (s *Server) projectSourceListContext(ctx context.Context, projectID, statusFilter string) ([]ProjectSource, error) {
	statusFilter, valid := parseArchivedStatusFilter(statusFilter)
	if !valid {
		statusFilter = "active"
	}
	rows, err := s.db.Query(ctx, `
		SELECT s.id, s.organization_id, s.project_id, s.source_type, s.title, s.content_format,
		       s.original_file_name, s.storage_key, s.status, s.metadata, s.revision,
		       s.content_revision, s.content_hash, s.created_by, s.created_at, s.updated_at,
		       COALESCE(c.chapter_count, 0),
		       COALESCE(c.first_volume_index, 0)
		FROM project_sources s
		LEFT JOIN (
			SELECT source_id,
			       count(*) AS chapter_count,
			       MIN(volume_index) FILTER (WHERE volume_index > 0) AS first_volume_index
			FROM novel_chapters
			WHERE project_id = $1
			GROUP BY source_id
		) c ON c.source_id = s.id
		WHERE s.project_id = $1
		  AND (
		    $2 = 'all'
		    OR ($2 = 'archived' AND COALESCE(s.status, 'ready') = 'archived')
		    OR ($2 = 'active' AND COALESCE(s.status, 'ready') <> 'archived')
		  )
		ORDER BY s.created_at ASC, s.title ASC
	`, projectID, statusFilter)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ProjectSource, 0)
	for rows.Next() {
		item, err := scanProjectSourceListItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sortProjectSources(items)
	return items, nil
}

func (s *Server) createProjectSource(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, err := s.project(r, r.PathValue("projectId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.authorizeAny(w, r, principal, []string{authz.PermissionSourceWrite, authz.PermissionProjectWrite}, authz.Resource{ProjectID: project.ID}) {
		return
	}
	if !s.enforceProjectRouteKind(w, r, project) {
		return
	}
	var req importProjectSourceRequest
	if !decode(w, r, &req) {
		return
	}
	req.ImportMethod = "paste"
	actionInput, err := json.Marshal(sourceCreateActionInput{
		SourceType: req.SourceType, Title: req.Title, Content: req.Content,
		ContentFormat: req.ContentFormat, OriginalFileName: req.OriginalFileName,
		StorageKey: req.StorageKey, Metadata: req.Metadata, Chapters: req.Chapters,
		SplitChapters: req.SplitChapters, CreateScript: req.CreateScript,
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	command, result, _, err := s.projectControl.executeManualSyncAction(
		r.Context(), principal, project, "source.create", actionInput, idempotencyKey(r, req.IdempotencyKey),
	)
	if err != nil {
		s.writeImportError(w, r, err)
		return
	}
	encoded, err := json.Marshal(result.Data)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var resp ImportProjectSourceResponse
	if err := json.Unmarshal(encoded, &resp); err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("X-CineWeave-Command-ID", command.ID)
	httpx.WriteJSON(w, r, http.StatusCreated, resp, nil)
}

func (s *Server) importProjectSourceFile(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, err := s.project(r, r.PathValue("projectId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.authorizeAny(w, r, principal, []string{authz.PermissionSourceWrite, authz.PermissionProjectWrite}, authz.Resource{ProjectID: project.ID}) {
		return
	}
	if !s.enforceProjectRouteKind(w, r, project) {
		return
	}
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "上传表单无效", nil, false)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "请选择要导入的文件", nil, false)
		return
	}
	defer file.Close()

	if !supportedImportFileName(header.Filename) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "UNSUPPORTED_FILE_TYPE", "当前仅支持 txt、md、markdown 文件。", nil, false)
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, 20<<20))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		title = strings.TrimSuffix(header.Filename, filepath.Ext(header.Filename))
	}
	contentFormat := strings.TrimSpace(r.FormValue("contentFormat"))
	if contentFormat == "" {
		contentFormat = contentFormatFromFileName(header.Filename)
	}
	splitChapters := optionalBoolFromForm(r.FormValue("splitChapters"))
	createScript := optionalBoolFromForm(r.FormValue("createScript"))
	resp, err := s.importProjectSource(r, principal, project, importProjectSourceRequest{
		SourceType:       r.FormValue("sourceType"),
		Title:            title,
		Content:          string(data),
		ContentFormat:    contentFormat,
		OriginalFileName: stringPtrFromValue(header.Filename),
		SplitChapters:    splitChapters,
		CreateScript:     createScript,
		ImportMethod:     "upload",
		FileName:         header.Filename,
		FileSize:         header.Size,
	})
	if err != nil {
		s.writeImportError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, resp, nil)
}

func (s *Server) importProjectSource(r *http.Request, principal auth.Principal, project Project, req importProjectSourceRequest) (ImportProjectSourceResponse, error) {
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		return ImportProjectSourceResponse{}, err
	}
	defer tx.Rollback(r.Context())
	response, err := s.createProjectSourceFromImportTx(r.Context(), tx, principal, project, req)
	if err != nil {
		return ImportProjectSourceResponse{}, err
	}
	if err := tx.Commit(r.Context()); err != nil {
		return ImportProjectSourceResponse{}, err
	}
	return response, nil
}

func (s *Server) getProjectSource(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionSourceRead)
	if !ok {
		return
	}
	item, err := s.projectSource(r, project.ID, r.PathValue("sourceId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	chapters, err := s.sourceChapters(r, project.ID, item.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	item.Chapters = chapters
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) listSourceChapters(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionSourceRead)
	if !ok {
		return
	}
	source, err := s.projectSource(r, project.ID, r.PathValue("sourceId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if source.SourceType != "novel" {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "sourceType must be novel", nil, false)
		return
	}
	items, err := s.sourceChapterSummaries(r, project.ID, source.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) getSourceChapter(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionSourceRead)
	if !ok {
		return
	}
	if _, err := s.projectSource(r, project.ID, r.PathValue("sourceId")); err != nil {
		s.writeError(w, r, err)
		return
	}
	item, err := s.sourceChapter(r, project.ID, r.PathValue("sourceId"), r.PathValue("chapterId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) deleteSourceChapter(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionSourceWrite)
	if !ok {
		return
	}
	var req struct {
		ExpectedRevision int64  `json:"expectedRevision"`
		IdempotencyKey   string `json:"idempotencyKey"`
		Reason           string `json:"reason"`
	}
	if !decode(w, r, &req) {
		return
	}
	actionInput := mustRawJSON(map[string]any{
		"sourceId": r.PathValue("sourceId"), "chapterId": r.PathValue("chapterId"),
		"expectedRevision": req.ExpectedRevision, "reason": req.Reason,
	})
	command, result, _, err := s.projectControl.executeManualSyncAction(
		r.Context(), principal, project, "source.delete_chapter", actionInput, idempotencyKey(r, req.IdempotencyKey),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("X-CineWeave-Command-ID", command.ID)
	httpx.WriteJSON(w, r, http.StatusOK, result.Data, nil)
}

func (s *Server) updateProjectSource(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionSourceWrite)
	if !ok {
		return
	}
	var req struct {
		ExpectedRevision int64                   `json:"expectedRevision"`
		IdempotencyKey   string                  `json:"idempotencyKey"`
		SourceType       *string                 `json:"sourceType"`
		Title            *string                 `json:"title"`
		Content          *string                 `json:"content"`
		ContentFormat    *string                 `json:"contentFormat"`
		OriginalFileName *string                 `json:"originalFileName"`
		StorageKey       *string                 `json:"storageKey"`
		Status           *string                 `json:"status"`
		Metadata         json.RawMessage         `json:"metadata"`
		Chapters         *[]sourceChapterRequest `json:"chapters"`
		SplitChapters    *bool                   `json:"splitChapters"`
	}
	if !decode(w, r, &req) {
		return
	}
	patch := map[string]any{}
	if req.SourceType != nil {
		patch["sourceType"] = *req.SourceType
	}
	if req.Title != nil {
		patch["title"] = *req.Title
	}
	if req.Content != nil {
		patch["content"] = *req.Content
	}
	if req.ContentFormat != nil {
		patch["contentFormat"] = *req.ContentFormat
	}
	if req.OriginalFileName != nil {
		patch["originalFileName"] = *req.OriginalFileName
	}
	if req.StorageKey != nil {
		patch["storageKey"] = *req.StorageKey
	}
	if req.Status != nil {
		patch["status"] = *req.Status
	}
	if len(req.Metadata) > 0 {
		patch["metadata"] = json.RawMessage(req.Metadata)
	}
	if req.Chapters != nil {
		patch["chapters"] = *req.Chapters
	}
	if req.SplitChapters != nil {
		patch["splitChapters"] = *req.SplitChapters
	}
	actionInput := mustRawJSON(map[string]any{
		"sourceId": r.PathValue("sourceId"), "expectedRevision": req.ExpectedRevision, "patch": patch,
	})
	command, result, _, err := s.projectControl.executeManualSyncAction(
		r.Context(), principal, project, "source.update", actionInput, idempotencyKey(r, req.IdempotencyKey),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	encodedSource, err := json.Marshal(result.Data["source"])
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var item ProjectSource
	if err := json.Unmarshal(encodedSource, &item); err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("X-CineWeave-Command-ID", command.ID)
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) deleteProjectSource(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionSourceWrite)
	if !ok {
		return
	}
	var req struct {
		ExpectedRevision int64  `json:"expectedRevision"`
		IdempotencyKey   string `json:"idempotencyKey"`
		Reason           string `json:"reason"`
	}
	if !decode(w, r, &req) {
		return
	}
	actionInput := mustRawJSON(map[string]any{
		"sourceId": r.PathValue("sourceId"), "expectedRevision": req.ExpectedRevision, "reason": req.Reason,
	})
	command, result, _, err := s.projectControl.executeManualSyncAction(
		r.Context(), principal, project, "source.delete", actionInput, idempotencyKey(r, req.IdempotencyKey),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("X-CineWeave-Command-ID", command.ID)
	httpx.WriteJSON(w, r, http.StatusOK, result.Data, nil)
}

func (s *Server) getProjectSourceImpact(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionSourceRead)
	if !ok {
		return
	}
	impact, err := s.projectSourceImpact(r, project.ID, r.PathValue("sourceId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, impact, nil)
}

func (s *Server) projectSourceImpact(r *http.Request, projectID, sourceID string) (OutputImpact, error) {
	if _, err := s.projectSource(r, projectID, sourceID); err != nil {
		return OutputImpact{}, err
	}
	var chapterCount, eventCount, planCount, scriptCount int
	if err := s.db.QueryRow(r.Context(), `
		SELECT
			(SELECT count(*) FROM novel_chapters WHERE project_id = $1 AND source_id = $2),
			(SELECT count(*) FROM novel_events WHERE project_id = $1 AND source_id = $2),
			(SELECT count(*) FROM adaptation_plans WHERE project_id = $1 AND source_id = $2),
			(SELECT count(*) FROM scripts WHERE project_id = $1 AND source_id = $2)
	`, projectID, sourceID).Scan(&chapterCount, &eventCount, &planCount, &scriptCount); err != nil {
		return OutputImpact{}, err
	}
	affected := make([]OutputImpactAffected, 0, 4)
	addAffected := func(entityType string, count int) {
		if count > 0 {
			affected = append(affected, OutputImpactAffected{EntityType: entityType, Count: count})
		}
	}
	addAffected("novel_chapter", chapterCount)
	addAffected("novel_event", eventCount)
	addAffected("adaptation_plan", planCount)
	addAffected("script", scriptCount)
	warnings := []string{"删除会将该内容归档并从默认列表隐藏，历史记录和已生成产物仍保留溯源。"}
	if scriptCount > 0 {
		warnings = append(warnings, "该内容已创建剧本，归档后剧本仍可在剧本页查看。")
	}
	if eventCount > 0 || planCount > 0 {
		warnings = append(warnings, "该内容已有事件或改编计划，后续重新生成可能需要重新选择内容。")
	}
	return OutputImpact{
		EntityType:      "source",
		EntityID:        sourceID,
		CanDelete:       true,
		RecommendedMode: "archive",
		DeleteModes:     []string{"archive"},
		Affected:        affected,
		Warnings:        warnings,
	}, nil
}

func (s *Server) markProjectSourceDownstreamStaleTx(ctx context.Context, tx pgx.Tx, project Project, sourceID string, changedFields []string, userID string) error {
	changedAt := time.Now().UTC()
	metadataPatch := map[string]any{
		"sourceChangedAt":   changedAt.Format(time.RFC3339),
		"downstreamStaleAt": changedAt.Format(time.RFC3339),
		"changedFields":     changedFields,
	}
	if _, err := tx.Exec(ctx, `
		UPDATE project_sources
		SET metadata = COALESCE(metadata, '{}'::jsonb) || $3::jsonb,
		    updated_at = now()
		WHERE project_id = $1 AND id = $2
	`, project.ID, sourceID, mustMarshal(metadataPatch)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE novel_chapters
		SET event_state = 'pending',
		    event_summary = NULL,
		    error_message = NULL,
		    updated_at = now()
		WHERE project_id = $1 AND source_id = $2
	`, project.ID, sourceID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE novel_events
		SET stale_state = 'needs_regeneration',
		    review_status = 'pending',
		    metadata = COALESCE(metadata, '{}'::jsonb) || $3::jsonb,
		    updated_at = now()
		WHERE project_id = $1 AND source_id = $2
	`, project.ID, sourceID, mustMarshal(metadataPatch)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE adaptation_plans
		SET status = CASE WHEN status = 'archived' THEN status ELSE 'draft' END,
		    review_status = 'pending',
		    metadata = COALESCE(metadata, '{}'::jsonb) || $3::jsonb,
		    updated_at = now()
		WHERE project_id = $1 AND source_id = $2
	`, project.ID, sourceID, mustMarshal(metadataPatch)); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `
		SELECT DISTINCT current_version_id::text
		FROM scripts
		WHERE project_id = $1
		  AND source_id = $2
		  AND current_version_id IS NOT NULL
	`, project.ID, sourceID)
	if err != nil {
		return err
	}
	versionIDs := make([]string, 0)
	for rows.Next() {
		var versionID string
		if err := rows.Scan(&versionID); err != nil {
			rows.Close()
			return err
		}
		versionIDs = append(versionIDs, versionID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, versionID := range versionIDs {
		if err := markScriptVersionDownstreamStale(ctx, tx, project.ID, versionID); err != nil {
			return err
		}
	}
	if err := production.MarkFinalVideoStale(ctx, tx, project.ID, ""); err != nil {
		return err
	}
	return insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "source.updated.downstream_stale", "project_source", sourceID, mustRawJSON(map[string]any{
		"sourceId":      sourceID,
		"changedFields": changedFields,
		"changedBy":     nullableMetadataValue(userID),
		"changedAt":     changedAt.Format(time.RFC3339),
	}))
}

func (s *Server) projectSource(r *http.Request, projectID, sourceID string) (ProjectSource, error) {
	return s.projectSourceContext(r.Context(), projectID, sourceID)
}

func (s *Server) projectSourceContext(ctx context.Context, projectID, sourceID string) (ProjectSource, error) {
	return scanProjectSource(s.db.QueryRow(ctx, `
		SELECT id, organization_id, project_id, source_type, title, content, content_format,
		       original_file_name, storage_key, status, metadata, revision, content_revision,
		       content_hash, created_by, created_at, updated_at
		FROM project_sources
		WHERE project_id = $1 AND id = $2
	`, projectID, sourceID))
}

func (s *Server) sourceChapters(r *http.Request, projectID, sourceID string) ([]NovelChapter, error) {
	return s.sourceChaptersContext(r.Context(), projectID, sourceID)
}

func (s *Server) sourceChaptersContext(ctx context.Context, projectID, sourceID string) ([]NovelChapter, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, source_id, chapter_index, volume_index, section_index, volume_title, chapter_title, content,
		       event_state, event_summary, error_message, created_at, updated_at
		FROM novel_chapters
		WHERE project_id = $1 AND source_id = $2
		ORDER BY COALESCE(volume_index, 0) ASC, COALESCE(section_index, chapter_index) ASC, chapter_index ASC
	`, projectID, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]NovelChapter, 0)
	for rows.Next() {
		item, err := scanNovelChapter(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) sourceChapterSummaries(r *http.Request, projectID, sourceID string) ([]NovelChapterSummary, error) {
	return s.sourceChapterSummariesContext(r.Context(), projectID, sourceID, 0, 0)
}

func (s *Server) sourceChapterSummariesContext(ctx context.Context, projectID, sourceID string, limit, offset int) ([]NovelChapterSummary, error) {
	if limit <= 0 {
		limit = 2147483647
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.Query(ctx, `
		WITH event_counts AS (
			SELECT chapter_id,
			       count(*) AS event_count,
			       count(*) FILTER (WHERE review_status = 'approved') AS approved_event_count,
			       count(*) FILTER (WHERE review_status <> 'approved') AS pending_event_review_count
			FROM novel_events
			WHERE project_id = $1
			GROUP BY chapter_id
		)
		SELECT c.id, c.source_id, c.chapter_index, c.volume_index, c.section_index, c.volume_title, c.chapter_title,
		       char_length(c.content), c.event_state, c.event_summary, c.error_message,
		       c.created_at, c.updated_at,
		       COALESCE(ec.event_count, 0),
		       COALESCE(ec.approved_event_count, 0),
		       COALESCE(ec.pending_event_review_count, 0)
		FROM novel_chapters c
		LEFT JOIN event_counts ec ON ec.chapter_id = c.id
		WHERE c.project_id = $1 AND c.source_id = $2
		ORDER BY COALESCE(c.volume_index, 0) ASC, COALESCE(c.section_index, c.chapter_index) ASC, c.chapter_index ASC
		LIMIT $3 OFFSET $4
	`, projectID, sourceID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]NovelChapterSummary, 0)
	for rows.Next() {
		item, err := scanNovelChapterSummary(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) sourceChapter(r *http.Request, projectID, sourceID, chapterID string) (NovelChapter, error) {
	return scanNovelChapter(s.db.QueryRow(r.Context(), `
		SELECT id, source_id, chapter_index, volume_index, section_index, volume_title, chapter_title, content,
		       event_state, event_summary, error_message, created_at, updated_at
		FROM novel_chapters
		WHERE project_id = $1 AND source_id = $2 AND id = $3
	`, projectID, sourceID, chapterID))
}

func (s *Server) replaceSourceChapters(ctx context.Context, tx pgx.Tx, project Project, sourceID string, reqChapters []sourceChapterRequest) ([]NovelChapter, error) {
	rows, err := tx.Query(ctx, `
		SELECT id::text, chapter_index, volume_index, section_index, volume_title, chapter_title, content
		FROM novel_chapters
		WHERE project_id = $1 AND source_id = $2
		ORDER BY COALESCE(volume_index, 0), COALESCE(section_index, chapter_index), chapter_index, id
		FOR UPDATE
	`, project.ID, sourceID)
	if err != nil {
		return nil, err
	}
	existing := make([]sourceChapterIdentityRecord, 0)
	for rows.Next() {
		var item sourceChapterIdentityRecord
		if err := rows.Scan(&item.ID, &item.ChapterIndex, &item.VolumeIndex, &item.SectionIndex, &item.VolumeTitle, &item.ChapterTitle, &item.Content); err != nil {
			rows.Close()
			return nil, err
		}
		existing = append(existing, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	existingByID := make(map[string]sourceChapterIdentityRecord, len(existing))
	for _, item := range existing {
		existingByID[item.ID] = item
	}
	claimed := make(map[string]bool, len(existing))
	items := make([]NovelChapter, 0, len(reqChapters))
	for i, chapter := range reqChapters {
		index := i + 1
		if chapter.ChapterIndex != nil && *chapter.ChapterIndex > 0 {
			index = *chapter.ChapterIndex
		}
		content := strings.TrimSpace(chapter.Content)
		if content == "" {
			continue
		}
		chapterID := strings.TrimSpace(stringValue(chapter.ID))
		if chapterID != "" {
			if _, ok := existingByID[chapterID]; !ok || claimed[chapterID] {
				return nil, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "chapter id does not belong to this source or is duplicated")
			}
		} else {
			chapterID = matchExistingSourceChapter(existing, claimed, chapter, index, content)
		}

		var item NovelChapter
		var err error
		if chapterID != "" {
			claimed[chapterID] = true
			item, err = scanNovelChapter(tx.QueryRow(ctx, `
				UPDATE novel_chapters
				SET chapter_index = $4,
				    volume_index = $5,
				    section_index = $6,
				    volume_title = $7,
				    chapter_title = $8,
				    content = $9,
				    event_state = CASE
				      WHEN ROW(chapter_index, volume_index, section_index, volume_title, chapter_title, content)
				        IS DISTINCT FROM ROW($4, $5::integer, $6::integer, $7::text, $8::text, $9::text)
				      THEN 'pending'
				      ELSE event_state
				    END,
				    updated_at = now()
				WHERE id = $1 AND project_id = $2 AND source_id = $3
				RETURNING id, source_id, chapter_index, volume_index, section_index, volume_title, chapter_title, content,
				          event_state, event_summary, error_message, created_at, updated_at
			`, chapterID, project.ID, sourceID, index, chapterVolumeIndex(chapter), chapterSectionIndex(chapter, index), chapter.VolumeTitle, chapter.ChapterTitle, content))
		} else {
			item, err = scanNovelChapter(tx.QueryRow(ctx, `
			INSERT INTO novel_chapters(
				organization_id, project_id, source_id, chapter_index, volume_index, section_index,
				volume_title, chapter_title, content, event_state
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'pending')
			RETURNING id, source_id, chapter_index, volume_index, section_index, volume_title, chapter_title, content,
			          event_state, event_summary, error_message, created_at, updated_at
			`, project.OrganizationID, project.ID, sourceID, index, chapterVolumeIndex(chapter), chapterSectionIndex(chapter, index), chapter.VolumeTitle, chapter.ChapterTitle, content))
		}
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	deletedIDs := make([]string, 0)
	for _, item := range existing {
		if !claimed[item.ID] {
			deletedIDs = append(deletedIDs, item.ID)
		}
	}
	if len(deletedIDs) > 0 {
		if _, err := tx.Exec(ctx, `
			DELETE FROM novel_chapters
			WHERE project_id = $1 AND source_id = $2 AND id = ANY($3::uuid[])
		`, project.ID, sourceID, deletedIDs); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func matchExistingSourceChapter(existing []sourceChapterIdentityRecord, claimed map[string]bool, chapter sourceChapterRequest, index int, content string) string {
	volumeIndex := intValueOrZero(chapter.VolumeIndex)
	sectionIndex := intValueOrZero(chapter.SectionIndex)
	volumeTitle := normalizeSourceChapterIdentity(stringValue(chapter.VolumeTitle))
	chapterTitle := normalizeSourceChapterIdentity(stringValue(chapter.ChapterTitle))
	content = strings.TrimSpace(content)
	bestID, bestScore := "", 0
	ambiguous := false
	for _, candidate := range existing {
		if claimed[candidate.ID] {
			continue
		}
		score := 0
		candidateVolumeTitle := normalizeSourceChapterIdentity(candidate.VolumeTitle.String)
		candidateChapterTitle := normalizeSourceChapterIdentity(candidate.ChapterTitle.String)
		if content != "" && strings.TrimSpace(candidate.Content) == content {
			score = 120
		}
		if chapterTitle != "" && chapterTitle == candidateChapterTitle {
			if volumeTitle != "" && volumeTitle == candidateVolumeTitle {
				score = max(score, 110)
			}
			if volumeIndex > 0 && candidate.VolumeIndex.Valid && int(candidate.VolumeIndex.Int32) == volumeIndex {
				score = max(score, 105)
			}
			if sectionIndex > 0 && candidate.SectionIndex.Valid && int(candidate.SectionIndex.Int32) == sectionIndex {
				score = max(score, 100)
			}
			if candidate.ChapterIndex == index {
				score = max(score, 90)
			}
		}
		if score == 0 {
			continue
		}
		if score > bestScore {
			bestID, bestScore, ambiguous = candidate.ID, score, false
		} else if score == bestScore {
			ambiguous = true
		}
	}
	if ambiguous {
		return ""
	}
	return bestID
}

func normalizeSourceChapterIdentity(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func intValueOrZero(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func scanProjectSource(row rowScan) (ProjectSource, error) {
	var item ProjectSource
	var originalFileName, storageKey, createdBy sql.NullString
	var metadata []byte
	err := row.Scan(
		&item.ID,
		&item.OrganizationID,
		&item.ProjectID,
		&item.SourceType,
		&item.Title,
		&item.Content,
		&item.ContentFormat,
		&originalFileName,
		&storageKey,
		&item.Status,
		&metadata,
		&item.Revision,
		&item.ContentRevision,
		&item.ContentHash,
		&createdBy,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	item.OriginalFileName = stringPtrFromNull(originalFileName)
	item.StorageKey = stringPtrFromNull(storageKey)
	item.CreatedBy = stringPtrFromNull(createdBy)
	item.Metadata = rawOrDefaultBytes(metadata, "{}")
	return item, err
}

func scanProjectSourceListItem(row rowScan) (ProjectSource, error) {
	var item ProjectSource
	var originalFileName, storageKey, createdBy sql.NullString
	var metadata []byte
	err := row.Scan(
		&item.ID,
		&item.OrganizationID,
		&item.ProjectID,
		&item.SourceType,
		&item.Title,
		&item.ContentFormat,
		&originalFileName,
		&storageKey,
		&item.Status,
		&metadata,
		&item.Revision,
		&item.ContentRevision,
		&item.ContentHash,
		&createdBy,
		&item.CreatedAt,
		&item.UpdatedAt,
		&item.ChapterCount,
		&item.FirstVolumeIndex,
	)
	item.OriginalFileName = stringPtrFromNull(originalFileName)
	item.StorageKey = stringPtrFromNull(storageKey)
	item.CreatedBy = stringPtrFromNull(createdBy)
	item.Metadata = rawOrDefaultBytes(metadata, "{}")
	return item, err
}

func scanNovelChapter(row rowScan) (NovelChapter, error) {
	var item NovelChapter
	var volumeTitle, chapterTitle, errorMessage sql.NullString
	var volumeIndex, sectionIndex sql.NullInt32
	var eventSummary []byte
	err := row.Scan(
		&item.ID,
		&item.SourceID,
		&item.ChapterIndex,
		&volumeIndex,
		&sectionIndex,
		&volumeTitle,
		&chapterTitle,
		&item.Content,
		&item.EventState,
		&eventSummary,
		&errorMessage,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	item.VolumeIndex = intPtrFromNullInt32(volumeIndex)
	item.SectionIndex = intPtrFromNullInt32(sectionIndex)
	item.VolumeTitle = stringPtrFromNull(volumeTitle)
	item.ChapterTitle = stringPtrFromNull(chapterTitle)
	item.EventSummary = rawOrDefaultBytes(eventSummary, "null")
	item.ErrorMessage = stringPtrFromNull(errorMessage)
	return item, err
}

func scanNovelChapterSummary(row rowScan) (NovelChapterSummary, error) {
	var item NovelChapterSummary
	var volumeTitle, chapterTitle, errorMessage sql.NullString
	var volumeIndex, sectionIndex sql.NullInt32
	var eventSummary []byte
	err := row.Scan(
		&item.ID,
		&item.SourceID,
		&item.ChapterIndex,
		&volumeIndex,
		&sectionIndex,
		&volumeTitle,
		&chapterTitle,
		&item.ContentLength,
		&item.EventState,
		&eventSummary,
		&errorMessage,
		&item.CreatedAt,
		&item.UpdatedAt,
		&item.EventCount,
		&item.ApprovedEventCount,
		&item.PendingEventReviewCount,
	)
	item.VolumeIndex = intPtrFromNullInt32(volumeIndex)
	item.SectionIndex = intPtrFromNullInt32(sectionIndex)
	item.VolumeTitle = stringPtrFromNull(volumeTitle)
	item.ChapterTitle = stringPtrFromNull(chapterTitle)
	item.EventSummary = rawOrDefaultBytes(eventSummary, "null")
	item.ErrorMessage = stringPtrFromNull(errorMessage)
	return item, err
}

func sortProjectSources(items []ProjectSource) {
	sort.SliceStable(items, func(i, j int) bool {
		left := items[i]
		right := items[j]
		if left.SourceType != right.SourceType {
			return sourceTypeSortWeight(left.SourceType) < sourceTypeSortWeight(right.SourceType)
		}
		leftRank := sourceReadOrderRank(left)
		rightRank := sourceReadOrderRank(right)
		if leftRank > 0 && rightRank > 0 && leftRank != rightRank {
			return leftRank < rightRank
		}
		if !left.CreatedAt.Equal(right.CreatedAt) {
			return left.CreatedAt.Before(right.CreatedAt)
		}
		return strings.ToLower(left.Title) < strings.ToLower(right.Title)
	})
}

func sourceReadOrderRank(source ProjectSource) int {
	if source.FirstVolumeIndex > 0 {
		return source.FirstVolumeIndex
	}
	return sourceTitleSortRank(source.Title)
}

func chapterVolumeIndex(chapter sourceChapterRequest) any {
	if chapter.VolumeIndex != nil && *chapter.VolumeIndex > 0 {
		return *chapter.VolumeIndex
	}
	if chapter.VolumeTitle != nil {
		if parsed := parseVolumeOrdinalFromText(*chapter.VolumeTitle); parsed > 0 {
			return parsed
		}
	}
	return nil
}

func chapterSectionIndex(chapter sourceChapterRequest, fallback int) any {
	if chapter.SectionIndex != nil && *chapter.SectionIndex > 0 {
		return *chapter.SectionIndex
	}
	if chapter.ChapterTitle != nil {
		if parsed := parseSectionOrdinalFromText(*chapter.ChapterTitle); parsed > 0 {
			return parsed
		}
	}
	if fallback > 0 {
		return fallback
	}
	return nil
}

func sourceTypeSortWeight(sourceType string) int {
	switch sourceType {
	case "novel":
		return 0
	case "script":
		return 1
	case "brief":
		return 2
	default:
		return 3
	}
}

func sourceTitleSortRank(title string) int {
	title = strings.TrimSpace(title)
	if title == "" {
		return 0
	}
	if value := leadingArabicNumber(title); value > 0 {
		return value
	}
	if value := chineseVolumeNumber(title); value > 0 {
		return value
	}
	return 0
}

func leadingArabicNumber(value string) int {
	value = strings.TrimSpace(value)
	digits := strings.Builder{}
	for _, r := range value {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
			continue
		}
		if r >= '０' && r <= '９' {
			digits.WriteRune('0' + (r - '０'))
			continue
		}
		break
	}
	if digits.Len() == 0 {
		return 0
	}
	parsed, err := strconv.Atoi(digits.String())
	if err != nil {
		return 0
	}
	return parsed
}

func chineseVolumeNumber(value string) int {
	value = strings.TrimSpace(value)
	start := strings.Index(value, "第")
	end := strings.Index(value, "卷")
	if start < 0 || end <= start {
		return 0
	}
	return chineseOrdinalNumber(value[start+len("第") : end])
}

func chineseOrdinalNumber(value string) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	digit := map[rune]int{
		'零': 0, '〇': 0,
		'一': 1, '壹': 1,
		'二': 2, '两': 2, '贰': 2,
		'三': 3, '叁': 3,
		'四': 4, '肆': 4,
		'五': 5, '伍': 5,
		'六': 6, '陆': 6,
		'七': 7, '柒': 7,
		'八': 8, '捌': 8,
		'九': 9, '玖': 9,
	}
	total := 0
	current := 0
	for _, r := range value {
		if n, ok := digit[r]; ok {
			current = n
			continue
		}
		switch r {
		case '十', '拾':
			if current == 0 {
				current = 1
			}
			total += current * 10
			current = 0
		case '百', '佰':
			if current == 0 {
				current = 1
			}
			total += current * 100
			current = 0
		default:
			return 0
		}
	}
	total += current
	return total
}

func validSourceType(value string) bool {
	return value == "novel" || value == "script" || value == "brief"
}

func validContentFormat(value string) bool {
	return value == "plain_text" || value == "markdown"
}

func validSourceStatus(value string) bool {
	return value == "ready" || value == "processing" || value == "processed" || value == "failed" || value == "archived"
}

func parseArchivedStatusFilter(value string) (string, bool) {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", "active":
		return "active", true
	case "archived":
		return "archived", true
	case "all":
		return "all", true
	default:
		return "", false
	}
}

func sourceChangedFields(current ProjectSource, nextSourceType, nextContent, nextContentFormat string, chaptersChanged bool) []string {
	fields := make([]string, 0, 4)
	if current.SourceType != nextSourceType {
		fields = append(fields, "sourceType")
	}
	if current.Content != nextContent {
		fields = append(fields, "content")
	}
	if current.ContentFormat != nextContentFormat {
		fields = append(fields, "contentFormat")
	}
	if chaptersChanged {
		fields = append(fields, "chapters")
	}
	return fields
}

func mergeRawObject(raw json.RawMessage, patch map[string]any) json.RawMessage {
	merged := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &merged)
	}
	for key, value := range patch {
		merged[key] = value
	}
	out, err := json.Marshal(merged)
	if err != nil {
		return rawOrDefault(raw, "{}")
	}
	return out
}

var errInvalidSourceImport = errors.New("invalid source import")

func (s *Server) writeImportError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, errInvalidSourceImport) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "sourceType、标题、正文或内容格式无效", nil, false)
		return
	}
	s.writeError(w, r, err)
}

func supportedImportFileName(fileName string) bool {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(fileName))) {
	case ".txt", ".md", ".markdown":
		return true
	default:
		return false
	}
}

func contentFormatFromFileName(fileName string) string {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(fileName))) {
	case ".md", ".markdown":
		return "markdown"
	default:
		return "plain_text"
	}
}

func optionalBoolFromForm(value string) *bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return nil
	}
	return &parsed
}

func shouldSplitChapters(sourceType string, value *bool) bool {
	if value != nil {
		return *value
	}
	return sourceType == "novel"
}

func shouldCreateScript(sourceType string, value *bool) bool {
	if value != nil {
		return *value
	}
	return sourceType == "script"
}

func stringPtrOrNil(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func intPtrOrNil(value int) *int {
	if value <= 0 {
		return nil
	}
	return &value
}

func intPtrFromNullInt32(value sql.NullInt32) *int {
	if !value.Valid {
		return nil
	}
	out := int(value.Int32)
	return &out
}

func importMethod(value string) string {
	switch strings.TrimSpace(value) {
	case "upload":
		return "upload"
	default:
		return "paste"
	}
}

func nullableMetadataValue(value any) any {
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return strings.TrimSpace(typed)
	case int64:
		if typed == 0 {
			return nil
		}
		return typed
	case int:
		if typed == 0 {
			return nil
		}
		return typed
	default:
		return value
	}
}

func mergeImportMetadata(raw json.RawMessage, importData map[string]any) (json.RawMessage, error) {
	metadata := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &metadata); err != nil {
			return nil, err
		}
	}
	cleanImport := map[string]any{}
	for key, value := range importData {
		if value != nil {
			cleanImport[key] = value
		}
	}
	metadata["import"] = cleanImport
	return json.Marshal(metadata)
}

func chapterSummaries(chapters []NovelChapter) []NovelChapterSummary {
	summaries := make([]NovelChapterSummary, 0, len(chapters))
	for _, chapter := range chapters {
		summaries = append(summaries, NovelChapterSummary{
			ID:            chapter.ID,
			SourceID:      chapter.SourceID,
			ChapterIndex:  chapter.ChapterIndex,
			VolumeIndex:   chapter.VolumeIndex,
			SectionIndex:  chapter.SectionIndex,
			VolumeTitle:   chapter.VolumeTitle,
			ChapterTitle:  chapter.ChapterTitle,
			ContentLength: len([]rune(chapter.Content)),
			EventState:    chapter.EventState,
			EventSummary:  rawOrDefault(chapter.EventSummary, `null`),
			ErrorMessage:  chapter.ErrorMessage,
			CreatedAt:     chapter.CreatedAt,
			UpdatedAt:     chapter.UpdatedAt,
		})
	}
	return summaries
}

func (s *Server) createImportedScript(r *http.Request, tx pgx.Tx, principal auth.Principal, project Project, sourceID, title, content, contentFormat, method string) (Script, ScriptVersion, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "导入剧本"
	}
	uniqueTitle, err := uniqueScriptTitleTx(r, tx, project.ID, title)
	if err != nil {
		return Script{}, ScriptVersion{}, err
	}
	sourceType := importMethod(method)
	metadata := json.RawMessage(mustMarshal(map[string]any{"sourceId": sourceID}))
	script, err := scanScript(tx.QueryRow(r.Context(), scriptInsertSQL(), project.OrganizationID, project.ID, &sourceID, uniqueTitle, "active", principal.UserID))
	if err != nil {
		return Script{}, ScriptVersion{}, err
	}
	version, err := insertScriptVersionTx(r, tx, project, script.ID, 1, content, contentFormat, &sourceType, "", "", metadata, principal.UserID)
	if err != nil {
		return Script{}, ScriptVersion{}, err
	}
	if _, err := insertScriptEpisodesTx(r, tx, project, script.ID, version.ID, principal.UserID, []scriptEpisodeDraft{
		defaultScriptEpisodeDraft(&sourceID, "第 1 集", content, contentFormat, "", "", "", metadata),
	}); err != nil {
		return Script{}, ScriptVersion{}, err
	}
	if _, err := tx.Exec(r.Context(), `UPDATE scripts SET current_version_id = $2, status = 'active' WHERE id = $1`, script.ID, version.ID); err != nil {
		return Script{}, ScriptVersion{}, err
	}
	if _, err := tx.Exec(r.Context(), `UPDATE projects SET active_script_id = $2 WHERE id = $1`, project.ID, script.ID); err != nil {
		return Script{}, ScriptVersion{}, err
	}
	script.CurrentVersionID = &version.ID
	script.IsCurrent = true
	script.CurrentVersion = &version
	return script, version, nil
}

func uniqueScriptTitleTx(r *http.Request, tx pgx.Tx, projectID, baseTitle string) (string, error) {
	baseTitle = strings.TrimSpace(baseTitle)
	if baseTitle == "" {
		baseTitle = "导入剧本"
	}
	for suffix := 1; suffix < 1000; suffix++ {
		candidate := baseTitle
		if suffix > 1 {
			candidate = fmt.Sprintf("%s（%d）", baseTitle, suffix)
		}
		var exists bool
		if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM scripts WHERE project_id = $1 AND title = $2)`, projectID, candidate).Scan(&exists); err != nil {
			return "", err
		}
		if !exists {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("script title conflict: %s", baseTitle)
}

func updateImportMetadataCreatedScript(r *http.Request, tx pgx.Tx, sourceID, scriptID string) error {
	_, err := tx.Exec(r.Context(), `
		UPDATE project_sources
		SET metadata = jsonb_set(COALESCE(metadata, '{}'::jsonb), '{import,createdScriptId}', to_jsonb($2::text), true)
		WHERE id = $1
	`, sourceID, scriptID)
	return err
}
