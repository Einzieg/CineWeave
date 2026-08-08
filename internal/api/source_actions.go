package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
	sourceutil "github.com/Einzieg/cineweave/internal/sources"
	"github.com/jackc/pgx/v5"
)

const sourceActionMaximumPageSize = 50

const sourceCreateMaximumInlineBytes = 48 * 1024
const sourceCreateMaximumCommandBytes = 60 * 1024

type sourceCreateActionInput struct {
	SourceType       string                 `json:"sourceType"`
	Title            string                 `json:"title"`
	Content          string                 `json:"content"`
	ContentFormat    string                 `json:"contentFormat"`
	OriginalFileName *string                `json:"originalFileName,omitempty"`
	StorageKey       *string                `json:"storageKey,omitempty"`
	Metadata         json.RawMessage        `json:"metadata,omitempty"`
	Chapters         []sourceChapterRequest `json:"chapters,omitempty"`
	SplitChapters    *bool                  `json:"splitChapters,omitempty"`
	CreateScript     *bool                  `json:"createScript,omitempty"`
}

type sourceListActionInput struct {
	Limit  int    `json:"limit"`
	Cursor string `json:"cursor"`
	Status string `json:"status"`
}

type sourceListChaptersActionInput struct {
	SourceID string `json:"sourceId"`
	Limit    int    `json:"limit"`
	Cursor   string `json:"cursor"`
}

type sourceActionSummary struct {
	ID               string    `json:"id"`
	SourceType       string    `json:"sourceType"`
	Title            string    `json:"title"`
	ContentFormat    string    `json:"contentFormat"`
	Status           string    `json:"status"`
	Revision         int64     `json:"revision"`
	ContentRevision  int64     `json:"contentRevision"`
	ContentHash      string    `json:"contentHash"`
	ChapterCount     int       `json:"chapterCount"`
	FirstVolumeIndex int       `json:"firstVolumeIndex,omitempty"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type sourceListActionPage struct {
	Items      []sourceActionSummary `json:"items"`
	Limit      int                   `json:"limit"`
	NextCursor string                `json:"nextCursor,omitempty"`
}

type sourceListChaptersActionPage struct {
	SourceID        string                `json:"sourceId"`
	SourceTitle     string                `json:"sourceTitle"`
	SourceRevision  int64                 `json:"sourceRevision"`
	ContentRevision int64                 `json:"contentRevision"`
	ContentHash     string                `json:"contentHash"`
	Items           []NovelChapterSummary `json:"items"`
	Limit           int                   `json:"limit"`
	NextCursor      string                `json:"nextCursor,omitempty"`
}

func decodeSourceCreateActionInput(raw json.RawMessage) (sourceCreateActionInput, error) {
	if len(raw) > sourceCreateMaximumCommandBytes {
		return sourceCreateActionInput{}, newAPIError(
			http.StatusRequestEntityTooLarge,
			"CONTENT_STAGING_REQUIRED",
			"创建请求超过命令记录上限，请先创建较短来源，再使用 content.write.begin/chunk/commit 分块写入",
		)
	}
	var input sourceCreateActionInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return sourceCreateActionInput{}, controlValidationError("source.create 输入格式无效")
	}
	input.SourceType = strings.TrimSpace(input.SourceType)
	input.Title = strings.TrimSpace(input.Title)
	input.ContentFormat = strings.TrimSpace(input.ContentFormat)
	if len([]byte(input.Content)) > sourceCreateMaximumInlineBytes {
		return sourceCreateActionInput{}, newAPIError(
			http.StatusRequestEntityTooLarge,
			"CONTENT_STAGING_REQUIRED",
			"正文超过内联创建上限，请先创建空来源，再使用 content.write.begin/chunk/commit 分块写入",
		)
	}
	return input, nil
}

func (input sourceCreateActionInput) importRequest() importProjectSourceRequest {
	return importProjectSourceRequest{
		SourceType: input.SourceType, Title: input.Title, Content: input.Content,
		ContentFormat: input.ContentFormat, OriginalFileName: input.OriginalFileName,
		StorageKey: input.StorageKey, Metadata: input.Metadata, Chapters: input.Chapters,
		SplitChapters: input.SplitChapters, CreateScript: input.CreateScript, ImportMethod: "paste",
	}
}

func (s *Server) createProjectSourceActionTx(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	input sourceCreateActionInput,
) (ImportProjectSourceResponse, error) {
	return s.createProjectSourceFromImportTx(ctx, tx, principal, project, input.importRequest())
}

func (s *Server) createProjectSourceFromImportTx(
	ctx context.Context,
	tx pgx.Tx,
	principal auth.Principal,
	project Project,
	req importProjectSourceRequest,
) (ImportProjectSourceResponse, error) {
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
		if err := s.authorizer.Authorize(ctx, principal, authz.PermissionScriptWrite, authz.Resource{ProjectID: project.ID}); err != nil {
			return ImportProjectSourceResponse{}, err
		}
	}

	chapterDrafts := make([]sourceChapterRequest, 0)
	if sourceType == "novel" && shouldSplitChapters(sourceType, req.SplitChapters) {
		for _, draft := range sourceutil.SplitNovelChapters(content) {
			chapterDrafts = append(chapterDrafts, sourceChapterRequest{
				ChapterIndex: &draft.Index, VolumeIndex: intPtrOrNil(draft.VolumeIndex),
				SectionIndex: intPtrOrNil(draft.SectionIndex), VolumeTitle: stringPtrOrNil(draft.VolumeTitle),
				ChapterTitle: stringPtrOrNil(draft.Title), Content: draft.Content,
			})
		}
	} else if len(req.Chapters) > 0 {
		chapterDrafts = req.Chapters
	}

	metadata, err := mergeImportMetadata(req.Metadata, map[string]any{
		"method": importMethod(req.ImportMethod), "fileName": nullableMetadataValue(req.FileName),
		"fileSize": nullableMetadataValue(req.FileSize), "contentLength": len([]rune(content)),
		"chapterCount": len(chapterDrafts),
	})
	if err != nil {
		return ImportProjectSourceResponse{}, err
	}
	item, err := scanProjectSource(tx.QueryRow(ctx, `
		INSERT INTO project_sources(
			organization_id, project_id, source_type, title, content, content_format,
			original_file_name, storage_key, status, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'processing', $9, $10)
		RETURNING id, organization_id, project_id, source_type, title, content, content_format,
		          original_file_name, storage_key, status, metadata, revision, content_revision,
		          content_hash, created_by, created_at, updated_at
	`, project.OrganizationID, project.ID, sourceType, title, content, contentFormat, req.OriginalFileName, req.StorageKey, metadata, principal.UserID))
	if err != nil {
		return ImportProjectSourceResponse{}, err
	}
	var chapters []NovelChapter
	if sourceType == "novel" && len(chapterDrafts) > 0 {
		chapters, err = s.replaceSourceChapters(ctx, tx, project, item.ID, chapterDrafts)
		if err != nil {
			return ImportProjectSourceResponse{}, err
		}
	}
	var scriptSummary *CreatedScriptSummary
	request := requestWithContext(ctx)
	if createScript {
		script, version, err := s.createImportedScript(request, tx, principal, project, item.ID, title, content, contentFormat, importMethod(req.ImportMethod))
		if err != nil {
			return ImportProjectSourceResponse{}, err
		}
		scriptSummary = &CreatedScriptSummary{ID: script.ID, CurrentVersionID: version.ID, Title: script.Title}
		if err := updateImportMetadataCreatedScript(request, tx, item.ID, script.ID); err != nil {
			return ImportProjectSourceResponse{}, err
		}
	}
	item, err = scanProjectSource(tx.QueryRow(ctx, `
		UPDATE project_sources
		SET status = 'processed'
		WHERE id = $1
		RETURNING id, organization_id, project_id, source_type, title, content, content_format,
		          original_file_name, storage_key, status, metadata, revision, content_revision,
		          content_hash, created_by, created_at, updated_at
	`, item.ID))
	if err != nil {
		return ImportProjectSourceResponse{}, err
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "source.created", "project_source", item.ID, mustRawJSON(map[string]any{
		"sourceId": item.ID, "sourceType": item.SourceType, "revision": item.Revision,
		"chapterCount": len(chapters), "createdBy": principal.UserID,
	})); err != nil {
		return ImportProjectSourceResponse{}, err
	}
	item.Chapters = nil
	item.Content = ""
	item.ChapterCount = len(chapters)
	for _, chapter := range chapters {
		if chapter.VolumeIndex != nil && *chapter.VolumeIndex > 0 {
			item.FirstVolumeIndex = *chapter.VolumeIndex
			break
		}
	}
	return ImportProjectSourceResponse{Source: item, Chapters: chapterSummaries(chapters), Script: scriptSummary}, nil
}

func sourceCreateAgentResult(arguments map[string]any, outcome ImportProjectSourceResponse) agentToolResult {
	source := outcome.Source
	source.Metadata = json.RawMessage(`{}`)
	chapters := outcome.Chapters
	chaptersTruncated := false
	if len(chapters) > sourceActionMaximumPageSize {
		chapters = chapters[:sourceActionMaximumPageSize]
		chaptersTruncated = true
	}
	return agentToolOK("source.create", arguments, fmt.Sprintf("已创建来源《%s》。", outcome.Source.Title), map[string]any{
		"source": source, "chapters": chapters, "script": outcome.Script,
		"chapterCount": outcome.Source.ChapterCount, "chaptersTruncated": chaptersTruncated,
	})
}

func (s *Server) listProjectSourcesAction(ctx context.Context, project Project, input sourceListActionInput) (sourceListActionPage, error) {
	limit, err := normalizeSourceActionPageLimit(input.Limit)
	if err != nil {
		return sourceListActionPage{}, err
	}
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = "active"
	}
	if _, valid := parseArchivedStatusFilter(status); !valid {
		return sourceListActionPage{}, controlValidationError("status 必须是 active、archived 或 all")
	}
	offset, err := decodeProjectControlOffsetCursor(input.Cursor)
	if err != nil {
		return sourceListActionPage{}, err
	}
	items, err := s.projectSourceListContext(ctx, project.ID, status)
	if err != nil {
		return sourceListActionPage{}, err
	}
	if offset > len(items) {
		return sourceListActionPage{}, controlValidationError("cursor 已失效，请重新读取来源列表")
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	summaries := make([]sourceActionSummary, 0, end-offset)
	for _, item := range items[offset:end] {
		summaries = append(summaries, sourceActionSummary{
			ID: item.ID, SourceType: item.SourceType, Title: item.Title,
			ContentFormat: item.ContentFormat, Status: item.Status,
			Revision: item.Revision, ContentRevision: item.ContentRevision, ContentHash: item.ContentHash,
			ChapterCount: item.ChapterCount, FirstVolumeIndex: item.FirstVolumeIndex,
			CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
		})
	}
	page := sourceListActionPage{Items: summaries, Limit: limit}
	if end < len(items) {
		page.NextCursor, err = encodeProjectControlOffsetCursor(end)
		if err != nil {
			return sourceListActionPage{}, err
		}
	}
	return page, nil
}

func (s *Server) listSourceChaptersAction(ctx context.Context, project Project, input sourceListChaptersActionInput) (sourceListChaptersActionPage, error) {
	input.SourceID = strings.TrimSpace(input.SourceID)
	if input.SourceID == "" {
		return sourceListChaptersActionPage{}, controlValidationError("sourceId 不能为空；请先使用 source.list 选择准确来源")
	}
	limit, err := normalizeSourceActionPageLimit(input.Limit)
	if err != nil {
		return sourceListChaptersActionPage{}, err
	}
	offset, err := decodeProjectControlOffsetCursor(input.Cursor)
	if err != nil {
		return sourceListChaptersActionPage{}, err
	}
	source, err := s.projectSourceContext(ctx, project.ID, input.SourceID)
	if err != nil {
		return sourceListChaptersActionPage{}, err
	}
	if source.SourceType != "novel" {
		return sourceListChaptersActionPage{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "只有小说来源包含分卷分集章节")
	}
	items, err := s.sourceChapterSummariesContext(ctx, project.ID, source.ID, limit+1, offset)
	if err != nil {
		return sourceListChaptersActionPage{}, err
	}
	page := sourceListChaptersActionPage{
		SourceID: source.ID, SourceTitle: source.Title, SourceRevision: source.Revision,
		ContentRevision: source.ContentRevision, ContentHash: source.ContentHash,
		Items: items, Limit: limit,
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		page.NextCursor, err = encodeProjectControlOffsetCursor(offset + limit)
		if err != nil {
			return sourceListChaptersActionPage{}, err
		}
	}
	return page, nil
}

func sourceListAgentResult(arguments map[string]any, page sourceListActionPage) agentToolResult {
	data := map[string]any{"items": page.Items, "limit": page.Limit}
	if page.NextCursor != "" {
		data["nextCursor"] = page.NextCursor
	}
	return agentToolOK("source.list", arguments, fmt.Sprintf("找到 %d 个原文/来源。", len(page.Items)), data)
}

func sourceListChaptersAgentResult(arguments map[string]any, page sourceListChaptersActionPage) agentToolResult {
	data := map[string]any{
		"sourceId": page.SourceID, "sourceTitle": page.SourceTitle,
		"sourceRevision": page.SourceRevision, "contentRevision": page.ContentRevision,
		"contentHash": page.ContentHash, "items": page.Items, "limit": page.Limit,
	}
	if page.NextCursor != "" {
		data["nextCursor"] = page.NextCursor
	}
	return agentToolOK("source.list_chapters", arguments, fmt.Sprintf("来源《%s》返回 %d 个分集/章节。", page.SourceTitle, len(page.Items)), data)
}

func normalizeSourceActionPageLimit(value int) (int, error) {
	if value == 0 {
		return 20, nil
	}
	if value < 1 || value > sourceActionMaximumPageSize {
		return 0, controlValidationError("limit 必须在 1 到 50 之间")
	}
	return value, nil
}

type sourceUpdateActionInput struct {
	SourceID         string
	ExpectedRevision int64
	Patch            sourceUpdateActionPatch
}

type sourceUpdateActionPatch struct {
	SourceType       *string
	Title            *string
	Content          *string
	ContentFormat    *string
	OriginalFileName *string
	StorageKey       *string
	Status           *string
	Metadata         json.RawMessage
	MetadataSet      bool
	Chapters         []sourceChapterRequest
	ChaptersSet      bool
	SplitChapters    *bool
}

type sourceUpdateActionOutcome struct {
	Source        ProjectSource `json:"source"`
	ChangedFields []string      `json:"changedFields"`
}

type sourceDeleteActionInput struct {
	SourceID         string `json:"sourceId"`
	ExpectedRevision int64  `json:"expectedRevision"`
	Reason           string `json:"reason"`
}

type sourceDeleteActionOutcome struct {
	Deleted  bool   `json:"deleted"`
	Mode     string `json:"mode"`
	SourceID string `json:"sourceId"`
	Revision int64  `json:"revision"`
}

type sourceDeleteChapterActionInput struct {
	SourceID         string `json:"sourceId"`
	ChapterID        string `json:"chapterId"`
	ExpectedRevision int64  `json:"expectedRevision"`
	Reason           string `json:"reason"`
}

type sourceDeleteChapterActionOutcome struct {
	DeleteSourceChapterResponse
	SourceRevision int64 `json:"sourceRevision"`
}

type sourceUpdateActionWire struct {
	SourceID         string `json:"sourceId"`
	ExpectedRevision int64  `json:"expectedRevision"`
	Patch            struct {
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
	} `json:"patch"`
}

func decodeSourceUpdateActionInput(raw json.RawMessage) (sourceUpdateActionInput, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return sourceUpdateActionInput{}, controlValidationError("source.update 输入必须是 JSON 对象")
	}
	var wire sourceUpdateActionWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return sourceUpdateActionInput{}, controlValidationError("source.update 输入格式无效")
	}
	patchRaw, exists := top["patch"]
	if !exists || string(patchRaw) == "null" {
		return sourceUpdateActionInput{}, controlValidationError("patch 不能为空")
	}
	var patchFields map[string]json.RawMessage
	if err := json.Unmarshal(patchRaw, &patchFields); err != nil || len(patchFields) == 0 {
		return sourceUpdateActionInput{}, controlValidationError("patch 必须是非空对象")
	}
	input := sourceUpdateActionInput{
		SourceID: strings.TrimSpace(wire.SourceID), ExpectedRevision: wire.ExpectedRevision,
		Patch: sourceUpdateActionPatch{
			SourceType: wire.Patch.SourceType, Title: wire.Patch.Title, Content: wire.Patch.Content,
			ContentFormat: wire.Patch.ContentFormat, OriginalFileName: wire.Patch.OriginalFileName,
			StorageKey: wire.Patch.StorageKey, Status: wire.Patch.Status,
			Metadata: wire.Patch.Metadata, Chapters: wire.Patch.Chapters,
			SplitChapters: wire.Patch.SplitChapters,
		},
	}
	_, input.Patch.MetadataSet = patchFields["metadata"]
	_, input.Patch.ChaptersSet = patchFields["chapters"]
	if input.SourceID == "" {
		return sourceUpdateActionInput{}, controlValidationError("sourceId 不能为空")
	}
	if input.ExpectedRevision < 1 {
		return sourceUpdateActionInput{}, controlValidationError("expectedRevision 必须大于 0")
	}
	return input, nil
}

func (s *Server) updateProjectSourceActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	actorUserID string,
	input sourceUpdateActionInput,
) (sourceUpdateActionOutcome, error) {
	current, err := scanProjectSource(tx.QueryRow(ctx, `
		SELECT id, organization_id, project_id, source_type, title, content, content_format,
		       original_file_name, storage_key, status, metadata, revision, content_revision,
		       content_hash, created_by, created_at, updated_at
		FROM project_sources
		WHERE project_id = $1 AND id = $2
		FOR UPDATE
	`, project.ID, input.SourceID))
	if err != nil {
		return sourceUpdateActionOutcome{}, err
	}
	if current.Revision != input.ExpectedRevision {
		return sourceUpdateActionOutcome{}, apiError{
			Status: http.StatusConflict, Code: "REVISION_CONFLICT",
			Message: "原文已被其它操作修改，请刷新后重试",
			Details: map[string]any{"expectedRevision": input.ExpectedRevision, "actualRevision": current.Revision},
		}
	}

	patch := input.Patch
	sourceType := current.SourceType
	if patch.SourceType != nil {
		sourceType = strings.TrimSpace(*patch.SourceType)
	}
	title := current.Title
	if patch.Title != nil {
		title = strings.TrimSpace(*patch.Title)
	}
	content := current.Content
	if patch.Content != nil {
		content = strings.TrimSpace(*patch.Content)
	}
	contentFormat := current.ContentFormat
	if patch.ContentFormat != nil {
		contentFormat = strings.TrimSpace(*patch.ContentFormat)
	}
	status := current.Status
	if patch.Status != nil {
		status = strings.TrimSpace(*patch.Status)
	}
	if !validSourceType(sourceType) || title == "" || content == "" || !validContentFormat(contentFormat) || !validSourceStatus(status) {
		return sourceUpdateActionOutcome{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "原文字段无效")
	}
	if patch.ChaptersSet && sourceType != "novel" {
		return sourceUpdateActionOutcome{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "只有小说原文可以显式设置章节")
	}

	metadata := current.Metadata
	if patch.MetadataSet {
		var object map[string]any
		if err := json.Unmarshal(patch.Metadata, &object); err != nil || object == nil {
			return sourceUpdateActionOutcome{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "metadata 必须是 JSON 对象")
		}
		metadata = mustRawJSON(object)
	}
	chaptersChanged := patch.ChaptersSet || (sourceType == "novel" && patch.Content != nil && shouldSplitChapters(sourceType, patch.SplitChapters))
	changedFields := sourceChangedFields(current, sourceType, content, contentFormat, chaptersChanged)

	item, err := scanProjectSource(tx.QueryRow(ctx, `
		UPDATE project_sources
		SET source_type = $3,
		    title = $4,
		    content = $5,
		    content_format = $6,
		    original_file_name = COALESCE($7, original_file_name),
		    storage_key = COALESCE($8, storage_key),
		    status = $9,
		    metadata = $10,
		    updated_at = now()
		WHERE id = $1 AND project_id = $2 AND revision = $11
		RETURNING id, organization_id, project_id, source_type, title, content, content_format,
		          original_file_name, storage_key, status, metadata, revision, content_revision,
		          content_hash, created_by, created_at, updated_at
	`, current.ID, project.ID, sourceType, title, content, contentFormat, patch.OriginalFileName,
		patch.StorageKey, status, metadata, input.ExpectedRevision))
	if err != nil {
		if err == pgx.ErrNoRows {
			return sourceUpdateActionOutcome{}, newAPIError(http.StatusConflict, "REVISION_CONFLICT", "原文已被其它操作修改，请刷新后重试")
		}
		return sourceUpdateActionOutcome{}, err
	}

	if patch.ChaptersSet {
		chapters, err := s.replaceSourceChapters(ctx, tx, project, item.ID, patch.Chapters)
		if err != nil {
			return sourceUpdateActionOutcome{}, err
		}
		item.Chapters = chapters
	} else if sourceType == "novel" && patch.Content != nil && shouldSplitChapters(sourceType, patch.SplitChapters) {
		chapterDrafts := make([]sourceChapterRequest, 0)
		for _, draft := range sourceutil.SplitNovelChapters(content) {
			chapterDrafts = append(chapterDrafts, sourceChapterRequest{
				ChapterIndex: &draft.Index, VolumeIndex: intPtrOrNil(draft.VolumeIndex),
				SectionIndex: intPtrOrNil(draft.SectionIndex), VolumeTitle: stringPtrOrNil(draft.VolumeTitle),
				ChapterTitle: stringPtrOrNil(draft.Title), Content: draft.Content,
			})
		}
		chapters, err := s.replaceSourceChapters(ctx, tx, project, item.ID, chapterDrafts)
		if err != nil {
			return sourceUpdateActionOutcome{}, err
		}
		item.Chapters = chapters
	}
	if len(changedFields) > 0 {
		if err := s.markProjectSourceDownstreamStaleTx(ctx, tx, project, item.ID, changedFields, actorUserID); err != nil {
			return sourceUpdateActionOutcome{}, err
		}
	}

	final, err := scanProjectSource(tx.QueryRow(ctx, `
		SELECT id, organization_id, project_id, source_type, title, content, content_format,
		       original_file_name, storage_key, status, metadata, revision, content_revision,
		       content_hash, created_by, created_at, updated_at
		FROM project_sources
		WHERE project_id = $1 AND id = $2
	`, project.ID, item.ID))
	if err != nil {
		return sourceUpdateActionOutcome{}, err
	}
	final.Chapters = item.Chapters
	return sourceUpdateActionOutcome{Source: final, ChangedFields: changedFields}, nil
}

func sourceUpdateAgentResult(arguments map[string]any, outcome sourceUpdateActionOutcome) agentToolResult {
	return agentToolOK("source.update", arguments, "已更新原文并同步下游状态。", map[string]any{
		"source": outcome.Source, "sourceId": outcome.Source.ID,
		"title": outcome.Source.Title, "status": outcome.Source.Status,
		"revision": outcome.Source.Revision, "changedFields": outcome.ChangedFields,
		"chapterCount": len(outcome.Source.Chapters),
	})
}

func decodeSourceDeleteActionInput(raw json.RawMessage) (sourceDeleteActionInput, error) {
	var input sourceDeleteActionInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return sourceDeleteActionInput{}, controlValidationError("source.delete 输入格式无效")
	}
	input.SourceID = strings.TrimSpace(input.SourceID)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.SourceID == "" {
		return sourceDeleteActionInput{}, controlValidationError("sourceId 不能为空")
	}
	if input.ExpectedRevision < 1 {
		return sourceDeleteActionInput{}, controlValidationError("expectedRevision 必须大于 0")
	}
	return input, nil
}

func (s *Server) deleteProjectSourceActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	actorUserID string,
	input sourceDeleteActionInput,
) (sourceDeleteActionOutcome, error) {
	current, err := scanProjectSource(tx.QueryRow(ctx, `
		SELECT id, organization_id, project_id, source_type, title, content, content_format,
		       original_file_name, storage_key, status, metadata, revision, content_revision,
		       content_hash, created_by, created_at, updated_at
		FROM project_sources
		WHERE project_id = $1 AND id = $2
		FOR UPDATE
	`, project.ID, input.SourceID))
	if err != nil {
		return sourceDeleteActionOutcome{}, err
	}
	if current.Revision != input.ExpectedRevision {
		return sourceDeleteActionOutcome{}, apiError{
			Status: http.StatusConflict, Code: "REVISION_CONFLICT",
			Message: "原文已被其它操作修改，请刷新后重试", Retryable: true,
			Details: map[string]any{"expectedRevision": input.ExpectedRevision, "actualRevision": current.Revision},
		}
	}
	archivedAt := time.Now().UTC().Format(time.RFC3339)
	metadataPatch := mustRawJSON(map[string]any{
		"archivedAt": archivedAt, "archivedBy": nullableMetadataValue(actorUserID), "reason": input.Reason,
	})
	var revision int64
	if err := tx.QueryRow(ctx, `
		UPDATE project_sources
		SET status = 'archived',
		    metadata = COALESCE(metadata, '{}'::jsonb) || $3::jsonb,
		    updated_at = now()
		WHERE project_id = $1 AND id = $2 AND revision = $4
		RETURNING revision
	`, project.ID, current.ID, metadataPatch, input.ExpectedRevision).Scan(&revision); err != nil {
		if err == pgx.ErrNoRows {
			return sourceDeleteActionOutcome{}, newAPIError(http.StatusConflict, "REVISION_CONFLICT", "原文已被其它操作修改，请刷新后重试")
		}
		return sourceDeleteActionOutcome{}, err
	}
	if err := s.markProjectSourceDownstreamStaleTx(ctx, tx, project, current.ID, []string{"status"}, actorUserID); err != nil {
		return sourceDeleteActionOutcome{}, err
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "source.archived", "project_source", current.ID, metadataPatch); err != nil {
		return sourceDeleteActionOutcome{}, err
	}
	if err := tx.QueryRow(ctx, `SELECT revision FROM project_sources WHERE id = $1`, current.ID).Scan(&revision); err != nil {
		return sourceDeleteActionOutcome{}, err
	}
	return sourceDeleteActionOutcome{Deleted: true, Mode: "archive", SourceID: current.ID, Revision: revision}, nil
}

func sourceDeleteAgentResult(arguments map[string]any, outcome sourceDeleteActionOutcome) agentToolResult {
	return agentToolOK("source.delete", arguments, "已归档原文并同步下游状态。", map[string]any{
		"deleted": outcome.Deleted, "mode": outcome.Mode, "sourceId": outcome.SourceID, "revision": outcome.Revision,
	})
}

func decodeSourceDeleteChapterActionInput(raw json.RawMessage) (sourceDeleteChapterActionInput, error) {
	var input sourceDeleteChapterActionInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return sourceDeleteChapterActionInput{}, controlValidationError("source.delete_chapter 输入格式无效")
	}
	input.SourceID = strings.TrimSpace(input.SourceID)
	input.ChapterID = strings.TrimSpace(input.ChapterID)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.SourceID == "" || input.ChapterID == "" {
		return sourceDeleteChapterActionInput{}, controlValidationError("sourceId 和 chapterId 不能为空")
	}
	if input.ExpectedRevision < 1 {
		return sourceDeleteChapterActionInput{}, controlValidationError("expectedRevision 必须大于 0")
	}
	return input, nil
}

func (s *Server) deleteSourceChapterActionTx(
	ctx context.Context,
	tx pgx.Tx,
	project Project,
	actorUserID string,
	input sourceDeleteChapterActionInput,
) (sourceDeleteChapterActionOutcome, error) {
	current, err := scanProjectSource(tx.QueryRow(ctx, `
		SELECT id, organization_id, project_id, source_type, title, content, content_format,
		       original_file_name, storage_key, status, metadata, revision, content_revision,
		       content_hash, created_by, created_at, updated_at
		FROM project_sources
		WHERE project_id = $1 AND id = $2
		FOR UPDATE
	`, project.ID, input.SourceID))
	if err != nil {
		return sourceDeleteChapterActionOutcome{}, err
	}
	if current.Revision != input.ExpectedRevision {
		return sourceDeleteChapterActionOutcome{}, apiError{
			Status: http.StatusConflict, Code: "REVISION_CONFLICT",
			Message: "原文已被其它操作修改，请刷新后重试", Retryable: true,
			Details: map[string]any{"expectedRevision": input.ExpectedRevision, "actualRevision": current.Revision},
		}
	}
	if current.SourceType != "novel" {
		return sourceDeleteChapterActionOutcome{}, newAPIError(http.StatusUnprocessableEntity, "VALIDATION_FAILED", "只有小说原文可以删除章节")
	}
	chapter, err := scanNovelChapter(tx.QueryRow(ctx, `
		SELECT id, source_id, chapter_index, volume_index, section_index, volume_title, chapter_title, content,
		       event_state, event_summary, error_message, created_at, updated_at
		FROM novel_chapters
		WHERE project_id = $1 AND source_id = $2 AND id = $3
		FOR UPDATE
	`, project.ID, current.ID, input.ChapterID))
	if err != nil {
		return sourceDeleteChapterActionOutcome{}, err
	}
	var chapterCount int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM novel_chapters WHERE project_id = $1 AND source_id = $2
	`, project.ID, current.ID).Scan(&chapterCount); err != nil {
		return sourceDeleteChapterActionOutcome{}, err
	}
	if chapterCount <= 1 {
		return sourceDeleteChapterActionOutcome{}, newAPIError(http.StatusConflict, "SOURCE_CHAPTER_LAST_REMAINING", "不能删除小说的最后一个章节；如需移除全部内容，请归档原文")
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM novel_chapters WHERE project_id = $1 AND source_id = $2 AND id = $3
	`, project.ID, current.ID, chapter.ID); err != nil {
		return sourceDeleteChapterActionOutcome{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE novel_chapters
		SET chapter_index = -chapter_index, updated_at = now()
		WHERE project_id = $1 AND source_id = $2 AND chapter_index > $3
	`, project.ID, current.ID, chapter.ChapterIndex); err != nil {
		return sourceDeleteChapterActionOutcome{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE novel_chapters
		SET chapter_index = -chapter_index - 1, updated_at = now()
		WHERE project_id = $1 AND source_id = $2 AND chapter_index < 0
	`, project.ID, current.ID); err != nil {
		return sourceDeleteChapterActionOutcome{}, err
	}
	var rebuiltContent string
	if err := tx.QueryRow(ctx, `
		WITH ordered AS (
			SELECT chapter_index,
			       NULLIF(btrim(volume_title), '') AS volume_title,
			       NULLIF(btrim(chapter_title), '') AS chapter_title,
			       NULLIF(btrim(content), '') AS content,
			       lag(NULLIF(btrim(volume_title), '')) OVER (ORDER BY chapter_index) AS previous_volume_title
			FROM novel_chapters
			WHERE project_id = $1 AND source_id = $2
		)
		SELECT COALESCE(string_agg(
			concat_ws(E'\n',
				CASE WHEN volume_title IS NOT NULL AND volume_title IS DISTINCT FROM previous_volume_title THEN volume_title END,
				chapter_title,
				content
			), E'\n\n' ORDER BY chapter_index
		), '')
		FROM ordered
	`, project.ID, current.ID).Scan(&rebuiltContent); err != nil {
		return sourceDeleteChapterActionOutcome{}, err
	}
	deletedAt := time.Now().UTC().Format(time.RFC3339)
	metadataPatch := map[string]any{
		"sourceChapterDeletedAt": deletedAt,
		"deletedChapterId":       chapter.ID, "deletedChapterIndex": chapter.ChapterIndex,
		"deletedBy": nullableMetadataValue(actorUserID), "reason": input.Reason,
	}
	if _, err := tx.Exec(ctx, `
		UPDATE project_sources
		SET content = $3,
		    metadata = COALESCE(metadata, '{}'::jsonb) || $4::jsonb,
		    updated_at = now()
		WHERE project_id = $1 AND id = $2 AND revision = $5
	`, project.ID, current.ID, rebuiltContent, mustMarshal(metadataPatch), input.ExpectedRevision); err != nil {
		return sourceDeleteChapterActionOutcome{}, err
	}
	if err := s.markProjectSourceDownstreamStaleTx(ctx, tx, project, current.ID, []string{"content", "chapters"}, actorUserID); err != nil {
		return sourceDeleteChapterActionOutcome{}, err
	}
	if err := insertAPIEvent(ctx, tx, project.OrganizationID, project.ID, "source.chapter.deleted", "novel_chapter", chapter.ID, mustRawJSON(map[string]any{
		"sourceId": current.ID, "chapterId": chapter.ID, "chapterIndex": chapter.ChapterIndex,
		"volumeIndex": nullableMetadataValue(chapter.VolumeIndex), "sectionIndex": nullableMetadataValue(chapter.SectionIndex),
		"chapterTitle": nullableMetadataValue(chapter.ChapterTitle), "remainingChapters": chapterCount - 1,
		"deletedBy": nullableMetadataValue(actorUserID), "deletedAt": deletedAt,
	})); err != nil {
		return sourceDeleteChapterActionOutcome{}, err
	}
	var revision int64
	if err := tx.QueryRow(ctx, `SELECT revision FROM project_sources WHERE id = $1`, current.ID).Scan(&revision); err != nil {
		return sourceDeleteChapterActionOutcome{}, err
	}
	return sourceDeleteChapterActionOutcome{
		DeleteSourceChapterResponse: DeleteSourceChapterResponse{
			Deleted: true, Mode: "delete_chapter", SourceID: current.ID, ChapterID: chapter.ID,
			DeletedChapterIndex: chapter.ChapterIndex, RemainingChapterCount: chapterCount - 1,
		},
		SourceRevision: revision,
	}, nil
}

func sourceDeleteChapterAgentResult(arguments map[string]any, outcome sourceDeleteChapterActionOutcome) agentToolResult {
	return agentToolOK("source.delete_chapter", arguments, "已删除原文章节并同步下游状态。", map[string]any{
		"deleted": outcome.Deleted, "mode": outcome.Mode, "sourceId": outcome.SourceID,
		"chapterId": outcome.ChapterID, "deletedChapterIndex": outcome.DeletedChapterIndex,
		"remainingChapterCount": outcome.RemainingChapterCount, "sourceRevision": outcome.SourceRevision,
	})
}

func sourceUpdateCommandOutput(arguments map[string]any, outcome sourceUpdateActionOutcome) (json.RawMessage, error) {
	return json.Marshal(sourceUpdateAgentResult(arguments, outcome))
}

func sourceUpdateActionArguments(input sourceUpdateActionInput) map[string]any {
	return map[string]any{
		"sourceId":         input.SourceID,
		"expectedRevision": input.ExpectedRevision,
	}
}

func validateSourceUpdateCommand(command projectcontrol.Command) error {
	if command.ProjectID == "" || command.ActorUserID == "" {
		return fmt.Errorf("source.update command identity is incomplete")
	}
	return nil
}
