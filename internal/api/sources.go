package api

import (
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
	sourceutil "github.com/Einzieg/cineweave/internal/sources"
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
	ChapterIndex *int    `json:"chapterIndex"`
	VolumeIndex  *int    `json:"volumeIndex"`
	SectionIndex *int    `json:"sectionIndex"`
	VolumeTitle  *string `json:"volumeTitle"`
	ChapterTitle *string `json:"chapterTitle"`
	Content      string  `json:"content"`
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
	ImportMethod     string                 `json:"-"`
	FileName         string                 `json:"-"`
	FileSize         int64                  `json:"-"`
}

func (s *Server) listProjectSources(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionSourceRead)
	if !ok {
		return
	}
	items, err := s.projectSourceList(r, project.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) projectSourceList(r *http.Request, projectID string) ([]ProjectSource, error) {
	rows, err := s.db.Query(r.Context(), `
		SELECT s.id, s.organization_id, s.project_id, s.source_type, s.title, s.content_format,
		       s.original_file_name, s.storage_key, s.status, s.metadata, s.created_by, s.created_at, s.updated_at,
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
		WHERE s.project_id = $1 AND s.status <> 'archived'
		ORDER BY s.created_at ASC, s.title ASC
	`, projectID)
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
	var req importProjectSourceRequest
	if !decode(w, r, &req) {
		return
	}
	req.ImportMethod = "paste"
	resp, err := s.importProjectSource(r, principal, project, req)
	if err != nil {
		s.writeImportError(w, r, err)
		return
	}
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
	sourceType := strings.TrimSpace(req.SourceType)
	title := strings.TrimSpace(req.Title)
	content := sourceutil.CleanImportedText(req.Content)
	contentFormat := strings.TrimSpace(req.ContentFormat)
	if contentFormat == "" {
		contentFormat = "plain_text"
	}
	if !validSourceType(sourceType) || title == "" || content == "" || !validContentFormat(contentFormat) {
		return ImportProjectSourceResponse{}, errInvalidSourceImport
	}
	createScript := shouldCreateScript(sourceType, req.CreateScript)
	if createScript {
		if err := s.authorizer.Authorize(r.Context(), principal, authz.PermissionScriptWrite, authz.Resource{ProjectID: project.ID}); err != nil {
			return ImportProjectSourceResponse{}, err
		}
	}

	chapterDrafts := make([]sourceChapterRequest, 0)
	if sourceType == "novel" && shouldSplitChapters(sourceType, req.SplitChapters) {
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
	} else if len(req.Chapters) > 0 {
		chapterDrafts = req.Chapters
	}

	metadata, err := mergeImportMetadata(req.Metadata, map[string]any{
		"method":        importMethod(req.ImportMethod),
		"fileName":      nullableMetadataValue(req.FileName),
		"fileSize":      nullableMetadataValue(req.FileSize),
		"contentLength": len([]rune(content)),
		"chapterCount":  len(chapterDrafts),
	})
	if err != nil {
		return ImportProjectSourceResponse{}, err
	}

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		return ImportProjectSourceResponse{}, err
	}
	defer tx.Rollback(r.Context())
	item, err := scanProjectSource(tx.QueryRow(r.Context(), `
		INSERT INTO project_sources(
			organization_id, project_id, source_type, title, content, content_format,
			original_file_name, storage_key, status, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'processing', $9, $10)
		RETURNING id, organization_id, project_id, source_type, title, content, content_format,
		          original_file_name, storage_key, status, metadata, created_by, created_at, updated_at
	`, project.OrganizationID, project.ID, sourceType, title, content, contentFormat, req.OriginalFileName, req.StorageKey, metadata, principal.UserID))
	if err != nil {
		return ImportProjectSourceResponse{}, err
	}
	var chapters []NovelChapter
	if sourceType == "novel" && len(chapterDrafts) > 0 {
		chapters, err = s.replaceSourceChapters(r, tx, project, item.ID, chapterDrafts)
		if err != nil {
			return ImportProjectSourceResponse{}, err
		}
		item.Chapters = chapters
	}
	var scriptSummary *CreatedScriptSummary
	if createScript {
		script, version, err := s.createImportedScript(r, tx, principal, project, item.ID, title, content, contentFormat, importMethod(req.ImportMethod))
		if err != nil {
			return ImportProjectSourceResponse{}, err
		}
		scriptSummary = &CreatedScriptSummary{ID: script.ID, CurrentVersionID: version.ID, Title: script.Title}
		if err := updateImportMetadataCreatedScript(r, tx, item.ID, script.ID); err != nil {
			return ImportProjectSourceResponse{}, err
		}
		item, err = scanProjectSource(tx.QueryRow(r.Context(), `
			UPDATE project_sources
			SET status = 'processed'
			WHERE id = $1
			RETURNING id, organization_id, project_id, source_type, title, content, content_format,
			          original_file_name, storage_key, status, metadata, created_by, created_at, updated_at
		`, item.ID))
		if err != nil {
			return ImportProjectSourceResponse{}, err
		}
	} else {
		item, err = scanProjectSource(tx.QueryRow(r.Context(), `
			UPDATE project_sources
			SET status = 'processed'
			WHERE id = $1
			RETURNING id, organization_id, project_id, source_type, title, content, content_format,
			          original_file_name, storage_key, status, metadata, created_by, created_at, updated_at
		`, item.ID))
		if err != nil {
			return ImportProjectSourceResponse{}, err
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		return ImportProjectSourceResponse{}, err
	}
	item.Chapters = chapters
	return ImportProjectSourceResponse{Source: item, Chapters: chapterSummaries(chapters), Script: scriptSummary}, nil
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

func (s *Server) updateProjectSource(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionSourceWrite)
	if !ok {
		return
	}
	current, err := s.projectSource(r, project.ID, r.PathValue("sourceId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var req struct {
		SourceType       *string                `json:"sourceType"`
		Title            *string                `json:"title"`
		Content          *string                `json:"content"`
		ContentFormat    *string                `json:"contentFormat"`
		OriginalFileName *string                `json:"originalFileName"`
		StorageKey       *string                `json:"storageKey"`
		Status           *string                `json:"status"`
		Metadata         json.RawMessage        `json:"metadata"`
		Chapters         []sourceChapterRequest `json:"chapters"`
		SplitChapters    *bool                  `json:"splitChapters"`
	}
	if !decode(w, r, &req) {
		return
	}
	sourceType := current.SourceType
	if req.SourceType != nil {
		sourceType = strings.TrimSpace(*req.SourceType)
	}
	title := current.Title
	if req.Title != nil {
		title = strings.TrimSpace(*req.Title)
	}
	content := current.Content
	if req.Content != nil {
		content = strings.TrimSpace(*req.Content)
	}
	contentFormat := current.ContentFormat
	if req.ContentFormat != nil {
		contentFormat = strings.TrimSpace(*req.ContentFormat)
	}
	status := current.Status
	if req.Status != nil {
		status = strings.TrimSpace(*req.Status)
	}
	if !validSourceType(sourceType) || title == "" || content == "" || !validContentFormat(contentFormat) || !validSourceStatus(status) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "source fields are invalid", nil, false)
		return
	}
	metadata := current.Metadata
	if len(req.Metadata) > 0 {
		var ok bool
		metadata, ok = jsonObjectOrDefault(w, r, req.Metadata)
		if !ok {
			return
		}
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	item, err := scanProjectSource(tx.QueryRow(r.Context(), `
		UPDATE project_sources
		SET source_type = $3,
		    title = $4,
		    content = $5,
		    content_format = $6,
		    original_file_name = COALESCE($7, original_file_name),
		    storage_key = COALESCE($8, storage_key),
		    status = $9,
		    metadata = $10
		WHERE id = $1 AND project_id = $2
		RETURNING id, organization_id, project_id, source_type, title, content, content_format,
		          original_file_name, storage_key, status, metadata, created_by, created_at, updated_at
	`, current.ID, project.ID, sourceType, title, content, contentFormat, req.OriginalFileName, req.StorageKey, status, metadata))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if req.Chapters != nil {
		chapters, err := s.replaceSourceChapters(r, tx, project, item.ID, req.Chapters)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		item.Chapters = chapters
	} else if sourceType == "novel" && req.Content != nil && shouldSplitChapters(sourceType, req.SplitChapters) {
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
			s.writeError(w, r, err)
			return
		}
		item.Chapters = chapters
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) deleteProjectSource(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionSourceWrite)
	if !ok {
		return
	}
	if _, err := s.projectSource(r, project.ID, r.PathValue("sourceId")); err != nil {
		s.writeError(w, r, err)
		return
	}
	if _, err := s.db.Exec(r.Context(), `
		UPDATE project_sources
		SET status = 'archived'
		WHERE project_id = $1 AND id = $2
	`, project.ID, r.PathValue("sourceId")); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"deleted": true, "mode": "archive"}, nil)
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

func (s *Server) projectSource(r *http.Request, projectID, sourceID string) (ProjectSource, error) {
	return scanProjectSource(s.db.QueryRow(r.Context(), `
		SELECT id, organization_id, project_id, source_type, title, content, content_format,
		       original_file_name, storage_key, status, metadata, created_by, created_at, updated_at
		FROM project_sources
		WHERE project_id = $1 AND id = $2
	`, projectID, sourceID))
}

func (s *Server) sourceChapters(r *http.Request, projectID, sourceID string) ([]NovelChapter, error) {
	rows, err := s.db.Query(r.Context(), `
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
	rows, err := s.db.Query(r.Context(), `
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
	`, projectID, sourceID)
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

func (s *Server) replaceSourceChapters(r *http.Request, tx pgx.Tx, project Project, sourceID string, reqChapters []sourceChapterRequest) ([]NovelChapter, error) {
	if _, err := tx.Exec(r.Context(), `DELETE FROM novel_chapters WHERE project_id = $1 AND source_id = $2`, project.ID, sourceID); err != nil {
		return nil, err
	}
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
		item, err := scanNovelChapter(tx.QueryRow(r.Context(), `
			INSERT INTO novel_chapters(
				organization_id, project_id, source_id, chapter_index, volume_index, section_index,
				volume_title, chapter_title, content, event_state
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'pending')
			RETURNING id, source_id, chapter_index, volume_index, section_index, volume_title, chapter_title, content,
			          event_state, event_summary, error_message, created_at, updated_at
		`, project.OrganizationID, project.ID, sourceID, index, chapterVolumeIndex(chapter), chapterSectionIndex(chapter, index), chapter.VolumeTitle, chapter.ChapterTitle, content))
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
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
	if _, err := tx.Exec(r.Context(), `UPDATE scripts SET current_version_id = $2, status = 'active' WHERE id = $1`, script.ID, version.ID); err != nil {
		return Script{}, ScriptVersion{}, err
	}
	script.CurrentVersionID = &version.ID
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
