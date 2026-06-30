package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/jackc/pgx/v5"
)

type ProjectSource struct {
	ID               string          `json:"id"`
	OrganizationID   string          `json:"organizationId"`
	ProjectID        string          `json:"projectId"`
	SourceType       string          `json:"sourceType"`
	Title            string          `json:"title"`
	Content          string          `json:"content"`
	ContentFormat    string          `json:"contentFormat"`
	OriginalFileName *string         `json:"originalFileName,omitempty"`
	StorageKey       *string         `json:"storageKey,omitempty"`
	Status           string          `json:"status"`
	Metadata         json.RawMessage `json:"metadata"`
	CreatedBy        *string         `json:"createdBy,omitempty"`
	CreatedAt        time.Time       `json:"createdAt"`
	UpdatedAt        time.Time       `json:"updatedAt"`
	Chapters         []NovelChapter  `json:"chapters,omitempty"`
}

type NovelChapter struct {
	ID           string          `json:"id"`
	SourceID     string          `json:"sourceId"`
	ChapterIndex int             `json:"chapterIndex"`
	VolumeTitle  *string         `json:"volumeTitle,omitempty"`
	ChapterTitle *string         `json:"chapterTitle,omitempty"`
	Content      string          `json:"content"`
	EventState   string          `json:"eventState"`
	EventSummary json.RawMessage `json:"eventSummary,omitempty"`
	ErrorMessage *string         `json:"errorMessage,omitempty"`
	CreatedAt    time.Time       `json:"createdAt"`
	UpdatedAt    time.Time       `json:"updatedAt"`
}

type sourceChapterRequest struct {
	ChapterIndex *int    `json:"chapterIndex"`
	VolumeTitle  *string `json:"volumeTitle"`
	ChapterTitle *string `json:"chapterTitle"`
	Content      string  `json:"content"`
}

func (s *Server) listProjectSources(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionSourceRead)
	if !ok {
		return
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT id, organization_id, project_id, source_type, title, content, content_format,
		       original_file_name, storage_key, status, metadata, created_by, created_at, updated_at
		FROM project_sources
		WHERE project_id = $1
		ORDER BY created_at DESC
	`, project.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]ProjectSource, 0)
	for rows.Next() {
		item, err := scanProjectSource(rows)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		items = append(items, item)
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) createProjectSource(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionSourceWrite)
	if !ok {
		return
	}
	var req struct {
		SourceType       string                 `json:"sourceType"`
		Title            string                 `json:"title"`
		Content          string                 `json:"content"`
		ContentFormat    string                 `json:"contentFormat"`
		OriginalFileName *string                `json:"originalFileName"`
		StorageKey       *string                `json:"storageKey"`
		Metadata         json.RawMessage        `json:"metadata"`
		Chapters         []sourceChapterRequest `json:"chapters"`
	}
	if !decode(w, r, &req) {
		return
	}
	sourceType := strings.TrimSpace(req.SourceType)
	title := strings.TrimSpace(req.Title)
	content := strings.TrimSpace(req.Content)
	contentFormat := strings.TrimSpace(req.ContentFormat)
	if contentFormat == "" {
		contentFormat = "plain_text"
	}
	if !validSourceType(sourceType) || title == "" || content == "" || !validContentFormat(contentFormat) {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "sourceType, title, content, and contentFormat are invalid", nil, false)
		return
	}
	metadata, ok := jsonObjectOrDefault(w, r, req.Metadata)
	if !ok {
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	item, err := scanProjectSource(tx.QueryRow(r.Context(), `
		INSERT INTO project_sources(
			organization_id, project_id, source_type, title, content, content_format,
			original_file_name, storage_key, status, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'ready', $9, $10)
		RETURNING id, organization_id, project_id, source_type, title, content, content_format,
		          original_file_name, storage_key, status, metadata, created_by, created_at, updated_at
	`, project.OrganizationID, project.ID, sourceType, title, content, contentFormat, req.OriginalFileName, req.StorageKey, metadata, principal.UserID))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if sourceType == "novel" && len(req.Chapters) > 0 {
		chapters, err := s.replaceSourceChapters(r, tx, project, item.ID, req.Chapters)
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
	httpx.WriteJSON(w, r, http.StatusCreated, item, nil)
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
	if _, err := s.db.Exec(r.Context(), `DELETE FROM project_sources WHERE project_id = $1 AND id = $2`, project.ID, r.PathValue("sourceId")); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]bool{"deleted": true}, nil)
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
		SELECT id, source_id, chapter_index, volume_title, chapter_title, content,
		       event_state, event_summary, error_message, created_at, updated_at
		FROM novel_chapters
		WHERE project_id = $1 AND source_id = $2
		ORDER BY chapter_index ASC
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
				organization_id, project_id, source_id, chapter_index, volume_title,
				chapter_title, content, event_state
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 'pending')
			RETURNING id, source_id, chapter_index, volume_title, chapter_title, content,
			          event_state, event_summary, error_message, created_at, updated_at
		`, project.OrganizationID, project.ID, sourceID, index, chapter.VolumeTitle, chapter.ChapterTitle, content))
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

func scanNovelChapter(row rowScan) (NovelChapter, error) {
	var item NovelChapter
	var volumeTitle, chapterTitle, errorMessage sql.NullString
	var eventSummary []byte
	err := row.Scan(
		&item.ID,
		&item.SourceID,
		&item.ChapterIndex,
		&volumeTitle,
		&chapterTitle,
		&item.Content,
		&item.EventState,
		&eventSummary,
		&errorMessage,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	item.VolumeTitle = stringPtrFromNull(volumeTitle)
	item.ChapterTitle = stringPtrFromNull(chapterTitle)
	item.EventSummary = rawOrDefaultBytes(eventSummary, "null")
	item.ErrorMessage = stringPtrFromNull(errorMessage)
	return item, err
}

func validSourceType(value string) bool {
	return value == "novel" || value == "script"
}

func validContentFormat(value string) bool {
	return value == "plain_text" || value == "markdown"
}

func validSourceStatus(value string) bool {
	return value == "ready" || value == "processing" || value == "processed" || value == "failed"
}
