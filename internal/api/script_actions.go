package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const scriptActionMaximumPageSize = 50

type scriptListActionInput struct {
	Status string `json:"status"`
	Limit  int    `json:"limit"`
	Cursor string `json:"cursor"`
}

type scriptGetActionInput struct {
	ScriptID      string `json:"scriptId"`
	VersionID     string `json:"versionId"`
	EpisodeLimit  int    `json:"episodeLimit"`
	EpisodeCursor string `json:"episodeCursor"`
}

type scriptActionSummary struct {
	ID                          string    `json:"id"`
	SourceID                    *string   `json:"sourceId,omitempty"`
	Title                       string    `json:"title"`
	Status                      string    `json:"status"`
	Revision                    int64     `json:"revision"`
	IsCurrent                   bool      `json:"isCurrent"`
	CurrentVersionID            *string   `json:"currentVersionId,omitempty"`
	CurrentVersion              int       `json:"currentVersion,omitempty"`
	CurrentVersionStatus        string    `json:"currentVersionStatus,omitempty"`
	CurrentVersionContentFormat string    `json:"currentVersionContentFormat,omitempty"`
	CurrentVersionContentLength int       `json:"currentVersionContentLength"`
	CurrentVersionContentHash   string    `json:"currentVersionContentHash,omitempty"`
	EpisodeCount                int       `json:"episodeCount"`
	CreatedAt                   time.Time `json:"createdAt"`
	UpdatedAt                   time.Time `json:"updatedAt"`
}

type scriptListActionPage struct {
	Items      []scriptActionSummary `json:"items"`
	Limit      int                   `json:"limit"`
	NextCursor string                `json:"nextCursor,omitempty"`
}

type scriptVersionActionSummary struct {
	ID            string                      `json:"id"`
	Version       int                         `json:"version"`
	Status        string                      `json:"status"`
	SourceType    *string                     `json:"sourceType,omitempty"`
	ContentFormat string                      `json:"contentFormat"`
	ContentLength int                         `json:"contentLength"`
	ContentHash   string                      `json:"contentHash"`
	CreatedAt     time.Time                   `json:"createdAt"`
	ContentTarget projectControlContentTarget `json:"contentTarget"`
}

type scriptEpisodeActionSummary struct {
	ID              string                      `json:"id"`
	SourceID        *string                     `json:"sourceId,omitempty"`
	SourceChapterID *string                     `json:"sourceChapterId,omitempty"`
	EpisodeIndex    int                         `json:"episodeIndex"`
	VolumeIndex     *int                        `json:"volumeIndex,omitempty"`
	SectionIndex    *int                        `json:"sectionIndex,omitempty"`
	VolumeTitle     *string                     `json:"volumeTitle,omitempty"`
	EpisodeTitle    string                      `json:"episodeTitle"`
	ContentFormat   string                      `json:"contentFormat"`
	ContentLength   int                         `json:"contentLength"`
	Revision        int64                       `json:"revision"`
	ContentHash     string                      `json:"contentHash"`
	ReviewStatus    string                      `json:"reviewStatus"`
	ManualOverride  bool                        `json:"manualOverride"`
	StaleState      string                      `json:"staleState"`
	UpdatedAt       time.Time                   `json:"updatedAt"`
	ContentTarget   projectControlContentTarget `json:"contentTarget"`
}

type scriptGetActionPage struct {
	Script            scriptActionSummary          `json:"script"`
	Version           *scriptVersionActionSummary  `json:"version,omitempty"`
	Episodes          []scriptEpisodeActionSummary `json:"episodes"`
	EpisodeLimit      int                          `json:"episodeLimit"`
	NextEpisodeCursor string                       `json:"nextEpisodeCursor,omitempty"`
}

type projectControlContentTarget struct {
	TargetType string `json:"targetType"`
	TargetID   string `json:"targetId"`
}

func (s *Server) listScriptsAction(ctx context.Context, project Project, input scriptListActionInput) (scriptListActionPage, error) {
	limit, err := normalizeProjectControlPageLimit(input.Limit, 20, scriptActionMaximumPageSize)
	if err != nil {
		return scriptListActionPage{}, err
	}
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = "active"
	}
	if _, valid := parseArchivedStatusFilter(status); !valid {
		return scriptListActionPage{}, controlValidationError("status 必须是 active、archived 或 all")
	}
	offset, err := decodeProjectControlOffsetCursor(input.Cursor)
	if err != nil {
		return scriptListActionPage{}, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT s.id::text, s.source_id::text, s.title, COALESCE(s.status, 'draft'), s.revision,
		       s.id = project.active_script_id, s.current_version_id::text,
		       COALESCE(version.version, 0), COALESCE(version.status, ''), COALESCE(version.content_format, ''),
		       COALESCE(char_length(version.content), 0),
		       COALESCE(encode(public.digest(pg_catalog.convert_to(version.content, 'UTF8'), 'sha256'), 'hex'), ''),
		       COALESCE(episodes.episode_count, 0), s.created_at, s.updated_at
		FROM scripts s
		JOIN projects project ON project.id = s.project_id
		LEFT JOIN script_versions version ON version.id = s.current_version_id
		LEFT JOIN LATERAL (
			SELECT count(*)::int AS episode_count
			FROM script_episodes episode
			WHERE episode.project_id = s.project_id
			  AND episode.script_id = s.id
			  AND episode.script_version_id = s.current_version_id
		) episodes ON true
		WHERE s.project_id = $1
		  AND (
			$2 = 'all'
			OR ($2 = 'archived' AND COALESCE(s.status, 'draft') = 'archived')
			OR ($2 = 'active' AND COALESCE(s.status, 'draft') <> 'archived')
		  )
		ORDER BY CASE WHEN s.id = project.active_script_id THEN 0 ELSE 1 END,
		         s.updated_at DESC, s.created_at DESC, s.id ASC
		LIMIT $3 OFFSET $4
	`, project.ID, status, limit+1, offset)
	if err != nil {
		return scriptListActionPage{}, err
	}
	defer rows.Close()
	items := make([]scriptActionSummary, 0, limit+1)
	for rows.Next() {
		var item scriptActionSummary
		var sourceID, currentVersionID sql.NullString
		if err := rows.Scan(
			&item.ID, &sourceID, &item.Title, &item.Status, &item.Revision,
			&item.IsCurrent, &currentVersionID, &item.CurrentVersion, &item.CurrentVersionStatus,
			&item.CurrentVersionContentFormat, &item.CurrentVersionContentLength,
			&item.CurrentVersionContentHash, &item.EpisodeCount, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return scriptListActionPage{}, err
		}
		item.SourceID = stringPtrFromNull(sourceID)
		item.CurrentVersionID = stringPtrFromNull(currentVersionID)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return scriptListActionPage{}, err
	}
	page := scriptListActionPage{Items: items, Limit: limit}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		page.NextCursor, err = encodeProjectControlOffsetCursor(offset + limit)
		if err != nil {
			return scriptListActionPage{}, err
		}
	}
	return page, nil
}

func (s *Server) getScriptAction(ctx context.Context, project Project, input scriptGetActionInput) (scriptGetActionPage, error) {
	input.ScriptID = strings.TrimSpace(input.ScriptID)
	input.VersionID = strings.TrimSpace(input.VersionID)
	if input.ScriptID == "" {
		return scriptGetActionPage{}, controlValidationError("scriptId 不能为空；请先使用 script.list 选择准确剧本")
	}
	limit, err := normalizeProjectControlPageLimit(input.EpisodeLimit, 20, scriptActionMaximumPageSize)
	if err != nil {
		return scriptGetActionPage{}, err
	}
	offset, err := decodeProjectControlOffsetCursor(input.EpisodeCursor)
	if err != nil {
		return scriptGetActionPage{}, err
	}
	script, err := s.scriptContext(ctx, project.ID, input.ScriptID)
	if err != nil {
		return scriptGetActionPage{}, err
	}
	page := scriptGetActionPage{
		Script: scriptActionSummary{
			ID: script.ID, SourceID: script.SourceID, Title: script.Title, Status: script.Status,
			Revision: script.Revision, IsCurrent: script.IsCurrent, CurrentVersionID: script.CurrentVersionID,
			CreatedAt: script.CreatedAt, UpdatedAt: script.UpdatedAt,
		},
		Episodes: []scriptEpisodeActionSummary{}, EpisodeLimit: limit,
	}
	versionID := input.VersionID
	if versionID == "" && script.CurrentVersionID != nil {
		versionID = *script.CurrentVersionID
	}
	if versionID == "" {
		if offset != 0 {
			return scriptGetActionPage{}, controlValidationError("episodeCursor 已失效，请重新读取剧本")
		}
		return page, nil
	}
	version, err := s.scriptVersionContext(ctx, project.ID, script.ID, versionID)
	if err != nil {
		return scriptGetActionPage{}, err
	}
	hash := sha256.Sum256([]byte(version.Content))
	versionSummary := scriptVersionActionSummary{
		ID: version.ID, Version: version.Version, Status: version.Status, SourceType: version.SourceType,
		ContentFormat: version.ContentFormat, ContentLength: len([]rune(version.Content)),
		ContentHash: hex.EncodeToString(hash[:]), CreatedAt: version.CreatedAt,
		ContentTarget: projectControlContentTarget{TargetType: "script_version", TargetID: version.ID},
	}
	page.Version = &versionSummary
	if script.CurrentVersionID != nil && *script.CurrentVersionID == version.ID {
		page.Script.CurrentVersion = version.Version
		page.Script.CurrentVersionStatus = version.Status
		page.Script.CurrentVersionContentFormat = version.ContentFormat
		page.Script.CurrentVersionContentLength = versionSummary.ContentLength
		page.Script.CurrentVersionContentHash = versionSummary.ContentHash
	}

	episodes, err := s.listScriptEpisodeSummariesContext(ctx, project.ID, script.ID, version.ID, limit+1, offset)
	if err != nil {
		return scriptGetActionPage{}, err
	}
	page.Episodes = episodes
	page.Script.EpisodeCount, err = s.scriptEpisodeCountContext(ctx, project.ID, script.ID, version.ID)
	if err != nil {
		return scriptGetActionPage{}, err
	}
	if len(page.Episodes) > limit {
		page.Episodes = page.Episodes[:limit]
		page.NextEpisodeCursor, err = encodeProjectControlOffsetCursor(offset + limit)
		if err != nil {
			return scriptGetActionPage{}, err
		}
	}
	return page, nil
}

func (s *Server) listScriptEpisodeSummariesContext(
	ctx context.Context,
	projectID, scriptID, versionID string,
	limit, offset int,
) ([]scriptEpisodeActionSummary, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id::text, source_id::text, source_chapter_id::text, episode_index,
		       volume_index, section_index, volume_title, episode_title, content_format,
		       char_length(content), revision, content_hash, review_status, manual_override,
		       stale_state, updated_at
		FROM script_episodes
		WHERE project_id = $1 AND script_id = $2 AND script_version_id = $3
		ORDER BY episode_index ASC, id ASC
		LIMIT $4 OFFSET $5
	`, projectID, scriptID, versionID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]scriptEpisodeActionSummary, 0, limit)
	for rows.Next() {
		var item scriptEpisodeActionSummary
		var sourceID, sourceChapterID sql.NullString
		if err := rows.Scan(
			&item.ID, &sourceID, &sourceChapterID, &item.EpisodeIndex,
			&item.VolumeIndex, &item.SectionIndex, &item.VolumeTitle, &item.EpisodeTitle,
			&item.ContentFormat, &item.ContentLength, &item.Revision, &item.ContentHash,
			&item.ReviewStatus, &item.ManualOverride, &item.StaleState, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		item.SourceID = stringPtrFromNull(sourceID)
		item.SourceChapterID = stringPtrFromNull(sourceChapterID)
		item.ContentTarget = projectControlContentTarget{TargetType: "script_episode", TargetID: item.ID}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) scriptEpisodeCountContext(ctx context.Context, projectID, scriptID, versionID string) (int, error) {
	var count int
	err := s.db.QueryRow(ctx, `
		SELECT count(*)::int
		FROM script_episodes
		WHERE project_id = $1 AND script_id = $2 AND script_version_id = $3
	`, projectID, scriptID, versionID).Scan(&count)
	return count, err
}

func scriptListAgentResult(arguments map[string]any, page scriptListActionPage) agentToolResult {
	data := map[string]any{"items": page.Items, "limit": page.Limit}
	if page.NextCursor != "" {
		data["nextCursor"] = page.NextCursor
	}
	return agentToolOK("script.list", arguments, fmt.Sprintf("找到 %d 个剧本。", len(page.Items)), data)
}

func scriptGetAgentResult(arguments map[string]any, page scriptGetActionPage) agentToolResult {
	data := map[string]any{
		"script": page.Script, "version": page.Version, "episodes": page.Episodes,
		"episodeLimit": page.EpisodeLimit,
	}
	if page.NextEpisodeCursor != "" {
		data["nextEpisodeCursor"] = page.NextEpisodeCursor
	}
	return agentToolOK("script.get", arguments, "已读取剧本《"+page.Script.Title+"》的结构化摘要。", data)
}

func decodeScriptListActionInput(raw json.RawMessage) (scriptListActionInput, error) {
	var input scriptListActionInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return scriptListActionInput{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "script.list 输入格式无效")
	}
	return input, nil
}

func decodeScriptGetActionInput(raw json.RawMessage) (scriptGetActionInput, error) {
	var input scriptGetActionInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return scriptGetActionInput{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "script.get 输入格式无效")
	}
	return input, nil
}
