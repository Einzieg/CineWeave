package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/jackc/pgx/v5"
)

type ScriptEpisode struct {
	ID              string          `json:"id"`
	OrganizationID  string          `json:"organizationId"`
	ProjectID       string          `json:"projectId"`
	ScriptID        string          `json:"scriptId"`
	ScriptVersionID string          `json:"scriptVersionId"`
	SourceID        *string         `json:"sourceId,omitempty"`
	SourceChapterID *string         `json:"sourceChapterId,omitempty"`
	EpisodeIndex    int             `json:"episodeIndex"`
	VolumeIndex     *int            `json:"volumeIndex,omitempty"`
	SectionIndex    *int            `json:"sectionIndex,omitempty"`
	VolumeTitle     *string         `json:"volumeTitle,omitempty"`
	EpisodeTitle    string          `json:"episodeTitle"`
	Content         string          `json:"content"`
	ContentFormat   string          `json:"contentFormat"`
	Revision        int64           `json:"revision"`
	ContentHash     string          `json:"contentHash"`
	PromptVersionID *string         `json:"promptVersionId,omitempty"`
	PromptHash      *string         `json:"promptHash,omitempty"`
	ProviderCallID  *string         `json:"providerCallId,omitempty"`
	ReviewStatus    string          `json:"reviewStatus"`
	ManualOverride  bool            `json:"manualOverride"`
	StaleState      string          `json:"staleState"`
	Metadata        json.RawMessage `json:"metadata"`
	CreatedBy       *string         `json:"createdBy,omitempty"`
	EditedBy        *string         `json:"editedBy,omitempty"`
	CreatedAt       time.Time       `json:"createdAt"`
	UpdatedAt       time.Time       `json:"updatedAt"`
	EditedAt        *time.Time      `json:"editedAt,omitempty"`
}

type scriptEpisodeDraft struct {
	SourceID        *string
	SourceChapterID *string
	EpisodeIndex    int
	VolumeIndex     *int
	SectionIndex    *int
	VolumeTitle     *string
	EpisodeTitle    string
	Content         string
	ContentFormat   string
	PromptVersionID string
	PromptHash      string
	ProviderCallID  string
	ReviewStatus    string
	StaleState      string
	Metadata        json.RawMessage
}

func defaultScriptEpisodeDraft(sourceID *string, title, content, contentFormat, promptVersionID, promptHash, providerCallID string, metadata json.RawMessage) scriptEpisodeDraft {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "第 1 集"
	}
	contentFormat = strings.TrimSpace(contentFormat)
	if contentFormat == "" {
		contentFormat = "markdown"
	}
	if len(metadata) == 0 {
		metadata = json.RawMessage(`{}`)
	}
	return scriptEpisodeDraft{
		SourceID:        sourceID,
		EpisodeIndex:    1,
		EpisodeTitle:    title,
		Content:         content,
		ContentFormat:   contentFormat,
		PromptVersionID: promptVersionID,
		PromptHash:      promptHash,
		ProviderCallID:  providerCallID,
		ReviewStatus:    "pending",
		StaleState:      "fresh",
		Metadata:        metadata,
	}
}

func scriptEpisodeDraftFromNovelChapter(index int, sourceID string, chapter scriptNovelChapterContext, content, promptVersionID, promptHash, providerCallID string, metadata json.RawMessage) scriptEpisodeDraft {
	if index <= 0 {
		index = chapter.ChapterIndex
	}
	if index <= 0 {
		index = 1
	}
	var volumeIndex *int
	if chapter.VolumeIndex > 0 {
		volumeIndex = &chapter.VolumeIndex
	}
	var sectionIndex *int
	if chapter.SectionIndex > 0 {
		sectionIndex = &chapter.SectionIndex
	}
	var volumeTitle *string
	if strings.TrimSpace(chapter.VolumeTitle) != "" {
		volumeTitle = stringPtrFromValue(strings.TrimSpace(chapter.VolumeTitle))
	}
	title := strings.TrimSpace(chapter.ChapterTitle)
	if title == "" {
		title = strings.TrimSpace(chapter.Title)
	}
	if title == "" {
		title = "第 " + intToString(index) + " 集"
	}
	sourceChapterID := chapter.ID
	return scriptEpisodeDraft{
		SourceID:        &sourceID,
		SourceChapterID: &sourceChapterID,
		EpisodeIndex:    index,
		VolumeIndex:     volumeIndex,
		SectionIndex:    sectionIndex,
		VolumeTitle:     volumeTitle,
		EpisodeTitle:    title,
		Content:         content,
		ContentFormat:   "markdown",
		PromptVersionID: promptVersionID,
		PromptHash:      promptHash,
		ProviderCallID:  providerCallID,
		ReviewStatus:    "pending",
		StaleState:      "fresh",
		Metadata:        metadata,
	}
}

func (s *Server) listScriptEpisodes(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptRead)
	if !ok {
		return
	}
	scriptID := r.PathValue("scriptId")
	versionID := r.PathValue("versionId")
	if _, err := s.script(r, project.ID, scriptID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if _, err := s.scriptVersion(r, project.ID, scriptID, versionID); err != nil {
		s.writeError(w, r, err)
		return
	}
	rows, err := s.db.Query(r.Context(), scriptEpisodeSelectSQL(`
		WHERE se.project_id = $1 AND se.script_id = $2 AND se.script_version_id = $3
		ORDER BY se.episode_index ASC
	`), project.ID, scriptID, versionID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	defer rows.Close()
	items := make([]ScriptEpisode, 0)
	for rows.Next() {
		item, err := scanScriptEpisode(rows)
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		s.writeError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) updateScriptEpisode(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionScriptWrite)
	if !ok {
		return
	}
	var req struct {
		ExpectedRevision int64           `json:"expectedRevision"`
		IdempotencyKey   string          `json:"idempotencyKey"`
		EpisodeTitle     *string         `json:"episodeTitle"`
		Content          *string         `json:"content"`
		ContentFormat    *string         `json:"contentFormat"`
		ReviewStatus     *string         `json:"reviewStatus"`
		StaleState       *string         `json:"staleState"`
		Metadata         json.RawMessage `json:"metadata"`
	}
	if !decode(w, r, &req) {
		return
	}
	patch := map[string]any{}
	if req.EpisodeTitle != nil {
		patch["episodeTitle"] = *req.EpisodeTitle
	}
	if req.Content != nil {
		patch["content"] = *req.Content
	}
	if req.ContentFormat != nil {
		patch["contentFormat"] = *req.ContentFormat
	}
	if req.ReviewStatus != nil {
		patch["reviewStatus"] = *req.ReviewStatus
	}
	if req.StaleState != nil {
		patch["staleState"] = *req.StaleState
	}
	if len(req.Metadata) > 0 {
		patch["metadata"] = req.Metadata
	}
	actionInput := mustRawJSON(map[string]any{
		"episodeId": r.PathValue("episodeId"), "expectedRevision": req.ExpectedRevision, "patch": patch,
	})
	command, _, _, err := s.projectControl.executeManualSyncAction(
		r.Context(), principal, project, "script.update_episode", actionInput, idempotencyKey(r, req.IdempotencyKey),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	item, err := s.scriptEpisode(r, project.ID, r.PathValue("episodeId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("X-CineWeave-Command-ID", command.ID)
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) scriptEpisode(r *http.Request, projectID, episodeID string) (ScriptEpisode, error) {
	return scanScriptEpisode(s.db.QueryRow(r.Context(), scriptEpisodeSelectSQL(`
		WHERE se.project_id = $1 AND se.id = $2
	`), projectID, episodeID))
}

func insertScriptEpisodesTx(r *http.Request, tx pgx.Tx, project Project, scriptID, versionID, createdBy string, drafts []scriptEpisodeDraft) ([]ScriptEpisode, error) {
	items := make([]ScriptEpisode, 0, len(drafts))
	seenSourceChapters := map[string]bool{}
	for i, draft := range drafts {
		if draft.SourceChapterID != nil {
			sourceChapterID := strings.TrimSpace(*draft.SourceChapterID)
			if sourceChapterID != "" {
				if seenSourceChapters[sourceChapterID] {
					return nil, newAPIError(http.StatusUnprocessableEntity, "DUPLICATE_SCRIPT_EPISODE_SOURCE_CHAPTER", "同一个剧本版本中，一个小说分集只能对应一个剧本分集")
				}
				seenSourceChapters[sourceChapterID] = true
			}
		}
		episodeIndex := draft.EpisodeIndex
		if episodeIndex <= 0 {
			episodeIndex = i + 1
		}
		title := strings.TrimSpace(draft.EpisodeTitle)
		if title == "" {
			title = "第 " + intToString(episodeIndex) + " 集"
		}
		contentFormat := strings.TrimSpace(draft.ContentFormat)
		if contentFormat == "" {
			contentFormat = "markdown"
		}
		reviewStatus := strings.TrimSpace(draft.ReviewStatus)
		if reviewStatus == "" {
			reviewStatus = "pending"
		}
		staleState := strings.TrimSpace(draft.StaleState)
		if staleState == "" {
			staleState = "fresh"
		}
		metadata := draft.Metadata
		if len(metadata) == 0 {
			metadata = json.RawMessage(`{}`)
		}
		item, err := scanScriptEpisode(tx.QueryRow(r.Context(), `
			INSERT INTO script_episodes(
				organization_id, project_id, script_id, script_version_id, source_id, source_chapter_id,
				episode_index, volume_index, section_index, volume_title, episode_title, content, content_format,
				prompt_version_id, prompt_hash, provider_call_id, review_status, stale_state, metadata, created_by
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
			        NULLIF($14, '')::uuid,
			        NULLIF($15, ''),
			        (SELECT id FROM provider_call_logs WHERE id = NULLIF($16, '')::uuid),
			        $17, $18, $19, $20)
			RETURNING id, organization_id, project_id, script_id, script_version_id, source_id, source_chapter_id,
			          episode_index, volume_index, section_index, volume_title, episode_title, content, content_format,
			          revision, content_hash,
			          prompt_version_id, prompt_hash, provider_call_id, review_status, manual_override, stale_state,
			          metadata, created_by, edited_by, created_at, updated_at, edited_at
		`, project.OrganizationID, project.ID, scriptID, versionID, draft.SourceID, draft.SourceChapterID,
			episodeIndex, draft.VolumeIndex, draft.SectionIndex, draft.VolumeTitle, title, strings.TrimSpace(draft.Content), contentFormat,
			draft.PromptVersionID, draft.PromptHash, draft.ProviderCallID, reviewStatus, staleState, metadata, createdBy))
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func rebuildScriptVersionContentFromEpisodesTx(r *http.Request, tx pgx.Tx, projectID, versionID string) error {
	rows, err := tx.Query(r.Context(), scriptEpisodeSelectSQL(`
		WHERE se.project_id = $1 AND se.script_version_id = $2
		ORDER BY se.episode_index ASC
	`), projectID, versionID)
	if err != nil {
		return err
	}
	defer rows.Close()
	episodes := make([]ScriptEpisode, 0)
	for rows.Next() {
		item, err := scanScriptEpisode(rows)
		if err != nil {
			return err
		}
		episodes = append(episodes, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(episodes) == 0 {
		return nil
	}
	content := scriptVersionContentFromEpisodes(episodes)
	_, err = tx.Exec(r.Context(), `
		UPDATE script_versions
		SET content = $3
		WHERE project_id = $1 AND id = $2
	`, projectID, versionID, content)
	return err
}

func scriptVersionContentFromEpisodeDrafts(drafts []scriptEpisodeDraft) string {
	episodes := make([]ScriptEpisode, 0, len(drafts))
	for i, draft := range drafts {
		index := draft.EpisodeIndex
		if index <= 0 {
			index = i + 1
		}
		title := strings.TrimSpace(draft.EpisodeTitle)
		if title == "" {
			title = "第 " + intToString(index) + " 集"
		}
		contentFormat := strings.TrimSpace(draft.ContentFormat)
		if contentFormat == "" {
			contentFormat = "markdown"
		}
		episodes = append(episodes, ScriptEpisode{
			EpisodeIndex:  index,
			VolumeIndex:   draft.VolumeIndex,
			SectionIndex:  draft.SectionIndex,
			VolumeTitle:   draft.VolumeTitle,
			EpisodeTitle:  title,
			Content:       strings.TrimSpace(draft.Content),
			ContentFormat: contentFormat,
		})
	}
	return scriptVersionContentFromEpisodes(episodes)
}

func scriptVersionContentFromEpisodes(episodes []ScriptEpisode) string {
	var builder strings.Builder
	for _, episode := range episodes {
		content := strings.TrimSpace(episode.Content)
		if content == "" {
			continue
		}
		if builder.Len() > 0 {
			builder.WriteString("\n\n")
		}
		builder.WriteString("## ")
		builder.WriteString(scriptEpisodeDisplayTitle(episode))
		builder.WriteString("\n\n")
		builder.WriteString(content)
	}
	return strings.TrimSpace(builder.String())
}

func scriptEpisodeDisplayTitle(episode ScriptEpisode) string {
	parts := make([]string, 0, 3)
	if episode.VolumeIndex != nil && *episode.VolumeIndex > 0 {
		parts = append(parts, "第 "+intToString(*episode.VolumeIndex)+"卷")
	}
	if episode.SectionIndex != nil && *episode.SectionIndex > 0 {
		parts = append(parts, "第 "+intToString(*episode.SectionIndex)+"节")
	} else if episode.EpisodeIndex > 0 {
		parts = append(parts, "第 "+intToString(episode.EpisodeIndex)+"集")
	}
	title := strings.TrimSpace(episode.EpisodeTitle)
	if title != "" {
		parts = append(parts, title)
	}
	if len(parts) == 0 {
		return "第 " + intToString(episode.EpisodeIndex) + " 集"
	}
	return strings.Join(parts, " ")
}

func scriptEpisodeSelectSQL(where string) string {
	return `
		SELECT se.id, se.organization_id, se.project_id, se.script_id, se.script_version_id,
		       se.source_id, se.source_chapter_id, se.episode_index, se.volume_index, se.section_index,
		       se.volume_title, se.episode_title, se.content, se.content_format,
		       se.revision, se.content_hash,
		       se.prompt_version_id, se.prompt_hash, se.provider_call_id,
		       se.review_status, se.manual_override, se.stale_state, se.metadata,
		       se.created_by, se.edited_by, se.created_at, se.updated_at, se.edited_at
		FROM script_episodes se
	` + where
}

func scanScriptEpisode(row rowScan) (ScriptEpisode, error) {
	var item ScriptEpisode
	var sourceID, sourceChapterID, volumeTitle, promptVersionID, promptHash, providerCallID, createdBy, editedBy sql.NullString
	var volumeIndex, sectionIndex sql.NullInt32
	var editedAt sql.NullTime
	var metadata []byte
	err := row.Scan(
		&item.ID,
		&item.OrganizationID,
		&item.ProjectID,
		&item.ScriptID,
		&item.ScriptVersionID,
		&sourceID,
		&sourceChapterID,
		&item.EpisodeIndex,
		&volumeIndex,
		&sectionIndex,
		&volumeTitle,
		&item.EpisodeTitle,
		&item.Content,
		&item.ContentFormat,
		&item.Revision,
		&item.ContentHash,
		&promptVersionID,
		&promptHash,
		&providerCallID,
		&item.ReviewStatus,
		&item.ManualOverride,
		&item.StaleState,
		&metadata,
		&createdBy,
		&editedBy,
		&item.CreatedAt,
		&item.UpdatedAt,
		&editedAt,
	)
	item.SourceID = stringPtrFromNull(sourceID)
	item.SourceChapterID = stringPtrFromNull(sourceChapterID)
	item.VolumeIndex = intPtrFromNullInt32(volumeIndex)
	item.SectionIndex = intPtrFromNullInt32(sectionIndex)
	item.VolumeTitle = stringPtrFromNull(volumeTitle)
	item.PromptVersionID = stringPtrFromNull(promptVersionID)
	item.PromptHash = stringPtrFromNull(promptHash)
	item.ProviderCallID = stringPtrFromNull(providerCallID)
	item.Metadata = rawOrDefaultBytes(metadata, "{}")
	item.CreatedBy = stringPtrFromNull(createdBy)
	item.EditedBy = stringPtrFromNull(editedBy)
	if editedAt.Valid {
		item.EditedAt = &editedAt.Time
	}
	return item, err
}

func validScriptReviewStatus(value string) bool {
	return value == "pending" || value == "approved" || value == "rejected" || value == "needs_edit"
}

func validScriptStaleState(value string) bool {
	return value == "fresh" || value == "upstream_changed" || value == "needs_regeneration"
}

func intToString(value int) string {
	return strconv.Itoa(value)
}
