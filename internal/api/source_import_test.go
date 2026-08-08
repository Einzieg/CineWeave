package api

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/db"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSourceImportRejectsUnsupportedFileType(t *testing.T) {
	server, seed := setupSourceImportTest(t)
	defer seed.Close()

	recorder := doMultipartAPIRequest(t, server, "/api/projects/"+seed.projectID+"/sources/import", seed.ownerToken, seed.organizationID, map[string]string{
		"sourceType": "novel",
		"title":      "PDF Source",
	}, "source.pdf", "not supported")
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d body=%s", recorder.Code, http.StatusUnprocessableEntity, recorder.Body.String())
	}
	var envelope httpx.Envelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope.Error == nil || envelope.Error.Code != "UNSUPPORTED_FILE_TYPE" || envelope.Error.Message != "当前仅支持 txt、md、markdown 文件。" {
		t.Fatalf("error = %#v", envelope.Error)
	}
}

func TestNovelImportGeneratesChapters(t *testing.T) {
	server, seed := setupSourceImportTest(t)
	defer seed.Close()

	var imported ImportProjectSourceResponse
	doMultipartAPISuccess(t, server, "/api/projects/"+seed.projectID+"/sources/import", seed.ownerToken, seed.organizationID, map[string]string{
		"sourceType": "novel",
		"title":      "原著第一卷",
	}, "novel.txt", "第一章 初见\n她推开门。\n\n第二章 远行\n他们出发。", &imported)
	if imported.Source.SourceType != "novel" || imported.Source.Status != "processed" {
		t.Fatalf("source = %+v", imported.Source)
	}
	if len(imported.Chapters) != 2 {
		t.Fatalf("chapters len = %d, want 2; response=%+v", len(imported.Chapters), imported.Chapters)
	}
	if imported.Chapters[0].ChapterIndex != 1 || stringValue(imported.Chapters[0].ChapterTitle) != "第一章 初见" {
		t.Fatalf("first chapter = %+v", imported.Chapters[0])
	}
	var count int
	if err := seed.pool.QueryRow(seed.ctx, `SELECT count(*) FROM novel_chapters WHERE project_id = $1 AND source_id = $2`, seed.projectID, imported.Source.ID).Scan(&count); err != nil {
		t.Fatalf("count chapters: %v", err)
	}
	if count != 2 {
		t.Fatalf("chapter count = %d, want 2", count)
	}
}

func TestDeleteSourceChapterRemovesCanonicalContentAndRenumbers(t *testing.T) {
	server, seed := setupSourceImportTest(t)
	defer seed.Close()

	var imported ImportProjectSourceResponse
	doMultipartAPISuccess(t, server, "/api/projects/"+seed.projectID+"/sources/import", seed.ownerToken, seed.organizationID, map[string]string{
		"sourceType": "novel",
		"title":      "可编辑小说",
	}, "novel.txt", "第一章 初见\n她推开门。\n\n第二章 远行\n他们出发。", &imported)
	if len(imported.Chapters) != 2 {
		t.Fatalf("chapters len = %d, want 2", len(imported.Chapters))
	}

	var deleted DeleteSourceChapterResponse
	doAPISuccess(t, server, http.MethodDelete,
		"/api/projects/"+seed.projectID+"/sources/"+imported.Source.ID+"/chapters/"+imported.Chapters[0].ID,
		seed.ownerToken, seed.organizationID, nil, &deleted)
	if !deleted.Deleted || deleted.DeletedChapterIndex != 1 || deleted.RemainingChapterCount != 1 {
		t.Fatalf("delete response = %+v", deleted)
	}

	var remaining struct {
		Items []NovelChapterSummary `json:"items"`
	}
	doAPISuccess(t, server, http.MethodGet,
		"/api/projects/"+seed.projectID+"/sources/"+imported.Source.ID+"/chapters",
		seed.ownerToken, seed.organizationID, nil, &remaining)
	if len(remaining.Items) != 1 || remaining.Items[0].ChapterIndex != 1 || stringValue(remaining.Items[0].ChapterTitle) != "第二章 远行" {
		t.Fatalf("remaining chapters = %+v", remaining.Items)
	}

	var source ProjectSource
	doAPISuccess(t, server, http.MethodGet,
		"/api/projects/"+seed.projectID+"/sources/"+imported.Source.ID,
		seed.ownerToken, seed.organizationID, nil, &source)
	if strings.Contains(source.Content, "她推开门") || !strings.Contains(source.Content, "第二章 远行") || !strings.Contains(source.Content, "他们出发") {
		t.Fatalf("rebuilt source content = %q", source.Content)
	}

	assertAPIErrorCode(t, server, http.MethodDelete,
		"/api/projects/"+seed.projectID+"/sources/"+imported.Source.ID+"/chapters/"+remaining.Items[0].ID,
		seed.ownerToken, seed.organizationID, nil, http.StatusConflict, "SOURCE_CHAPTER_LAST_REMAINING")
}

func TestNovelImportPersistsVolumeAndSectionOrdinals(t *testing.T) {
	server, seed := setupSourceImportTest(t)
	defer seed.Close()

	var imported ImportProjectSourceResponse
	doMultipartAPISuccess(t, server, "/api/projects/"+seed.projectID+"/sources/import", seed.ownerToken, seed.organizationID, map[string]string{
		"sourceType": "novel",
		"title":      "蛊真人第一卷",
	}, "novel.txt", "第一卷：魔性不改\n第一节：纵身亡魔心仍不悔\nA\n第二节：逆光阴五百年觉悟\nB", &imported)
	if len(imported.Chapters) != 2 {
		t.Fatalf("chapters len = %d, want 2; response=%+v", len(imported.Chapters), imported.Chapters)
	}
	if intValue(imported.Chapters[0].VolumeIndex) != 1 || intValue(imported.Chapters[0].SectionIndex) != 1 {
		t.Fatalf("first chapter ordinals = %+v", imported.Chapters[0])
	}
	if intValue(imported.Chapters[1].VolumeIndex) != 1 || intValue(imported.Chapters[1].SectionIndex) != 2 {
		t.Fatalf("second chapter ordinals = %+v", imported.Chapters[1])
	}

	var volumeIndex, sectionIndex int
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT COALESCE(volume_index, 0), COALESCE(section_index, 0)
		FROM novel_chapters
		WHERE project_id = $1 AND source_id = $2
		ORDER BY chapter_index ASC
		LIMIT 1
	`, seed.projectID, imported.Source.ID).Scan(&volumeIndex, &sectionIndex); err != nil {
		t.Fatalf("read persisted ordinals: %v", err)
	}
	if volumeIndex != 1 || sectionIndex != 1 {
		t.Fatalf("persisted ordinals = %d/%d, want 1/1", volumeIndex, sectionIndex)
	}
}

func TestChapterSummariesPreserveVolumeAndSectionOrdinals(t *testing.T) {
	volumeIndex := 1
	sectionIndex := 2
	volumeTitle := "第一卷：魔性不改"
	chapterTitle := "第二节：逆光阴五百年觉悟"
	summaries := chapterSummaries([]NovelChapter{{
		ID:           "chapter-1",
		SourceID:     "source-1",
		ChapterIndex: 2,
		VolumeIndex:  &volumeIndex,
		SectionIndex: &sectionIndex,
		VolumeTitle:  &volumeTitle,
		ChapterTitle: &chapterTitle,
		Content:      "B",
		EventState:   "pending",
	}})
	if len(summaries) != 1 {
		t.Fatalf("summaries len = %d, want 1", len(summaries))
	}
	if intValue(summaries[0].VolumeIndex) != 1 || intValue(summaries[0].SectionIndex) != 2 {
		t.Fatalf("summary ordinals = %+v", summaries[0])
	}
}

func TestScriptImportCreatesScriptAndVersion(t *testing.T) {
	server, seed := setupSourceImportTest(t)
	defer seed.Close()

	var imported ImportProjectSourceResponse
	doMultipartAPISuccess(t, server, "/api/projects/"+seed.projectID+"/sources/import", seed.ownerToken, seed.organizationID, map[string]string{
		"sourceType": "script",
		"title":      "第一版剧本",
	}, "script.md", "# 第一场\n\n角色进入房间。", &imported)
	if imported.Source.SourceType != "script" || imported.Source.Status != "processed" {
		t.Fatalf("source = %+v", imported.Source)
	}
	if imported.Script == nil || imported.Script.ID == "" || imported.Script.CurrentVersionID == "" {
		t.Fatalf("script summary = %+v", imported.Script)
	}
	var version int
	var content, sourceType string
	var metadata json.RawMessage
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT version, content, source_type, metadata
		FROM script_versions
		WHERE id = $1 AND script_id = $2
	`, imported.Script.CurrentVersionID, imported.Script.ID).Scan(&version, &content, &sourceType, &metadata); err != nil {
		t.Fatalf("query script version: %v", err)
	}
	if version != 1 || content != "# 第一场\n\n角色进入房间。" || sourceType != "upload" {
		t.Fatalf("version/content/sourceType = %d/%q/%q", version, content, sourceType)
	}
	var meta struct {
		SourceID string `json:"sourceId"`
	}
	if err := json.Unmarshal(metadata, &meta); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if meta.SourceID != imported.Source.ID {
		t.Fatalf("metadata sourceId = %s, want %s", meta.SourceID, imported.Source.ID)
	}
	var episodeCount int
	var episodeContent string
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT count(*), COALESCE(max(content), '')
		FROM script_episodes
		WHERE project_id = $1 AND script_id = $2 AND script_version_id = $3
	`, seed.projectID, imported.Script.ID, imported.Script.CurrentVersionID).Scan(&episodeCount, &episodeContent); err != nil {
		t.Fatalf("query script episodes: %v", err)
	}
	if episodeCount != 1 || episodeContent != "# 第一场\n\n角色进入房间。" {
		t.Fatalf("episode count/content = %d/%q", episodeCount, episodeContent)
	}
}

func TestBriefImportCreatesSourceOnly(t *testing.T) {
	server, seed := setupSourceImportTest(t)
	defer seed.Close()

	var imported ImportProjectSourceResponse
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/sources", seed.ownerToken, seed.organizationID, map[string]any{
		"sourceType":     "brief",
		"title":          "创意文案",
		"content":        "一个关于海边旧灯塔的悬疑短片。",
		"idempotencyKey": "test-create-brief-source",
	}, &imported)
	if imported.Source.SourceType != "brief" || imported.Source.Status != "processed" {
		t.Fatalf("source = %+v", imported.Source)
	}
	if imported.Script != nil {
		t.Fatalf("script summary = %+v, want nil", imported.Script)
	}
	var chapterCount, scriptCount int
	if err := seed.pool.QueryRow(seed.ctx, `SELECT count(*) FROM novel_chapters WHERE project_id = $1 AND source_id = $2`, seed.projectID, imported.Source.ID).Scan(&chapterCount); err != nil {
		t.Fatalf("count chapters: %v", err)
	}
	if err := seed.pool.QueryRow(seed.ctx, `SELECT count(*) FROM scripts WHERE project_id = $1 AND source_id = $2`, seed.projectID, imported.Source.ID).Scan(&scriptCount); err != nil {
		t.Fatalf("count scripts: %v", err)
	}
	if chapterCount != 0 || scriptCount != 0 {
		t.Fatalf("chapter/script count = %d/%d, want 0/0", chapterCount, scriptCount)
	}
	var controllerType, commandStatus string
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT controller_type, status
		FROM project_control_commands
		WHERE project_id = $1 AND action_name = 'source.create' AND idempotency_key = $2
	`, seed.projectID, "test-create-brief-source").Scan(&controllerType, &commandStatus); err != nil {
		t.Fatalf("read source.create command: %v", err)
	}
	if controllerType != "manual" || commandStatus != "succeeded" {
		t.Fatalf("source.create command = %s/%s", controllerType, commandStatus)
	}
}

func TestSourceImportRequiresPermission(t *testing.T) {
	server, seed := setupSourceImportTest(t)
	defer seed.Close()

	recorder := doMultipartAPIRequest(t, server, "/api/projects/"+seed.projectID+"/sources/import", seed.otherToken, seed.organizationID, map[string]string{
		"sourceType": "novel",
		"title":      "Denied",
	}, "novel.txt", "第一章\n正文")
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d body=%s", recorder.Code, http.StatusForbidden, recorder.Body.String())
	}
	var envelope httpx.Envelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope.Error == nil || envelope.Error.Code != "ACCESS_DENIED" {
		t.Fatalf("error = %#v", envelope.Error)
	}
}

type sourceImportSeed struct {
	ctx                 context.Context
	pool                *pgxpool.Pool
	organizationID      string
	otherOrganizationID string
	ownerToken          string
	otherToken          string
	projectID           string
}

func setupSourceImportTest(t *testing.T) (http.Handler, *sourceImportSeed) {
	t.Helper()
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run source import API tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for source import API tests")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	authService := auth.NewService(pool, "source-import-test-secret", time.Hour, 24*time.Hour)
	server := New(pool, authService, nil, nil, nil).Handler()
	suffix := uuid.NewString()
	owner, err := authService.Register(ctx, auth.RegisterRequest{
		Email:            "source-import-owner-" + suffix + "@example.test",
		Username:         randomStorageSegment(),
		Password:         "Password123!",
		DisplayName:      "Source Import Owner",
		OrganizationName: "Source Import Org " + suffix,
		WorkspaceName:    "Source Import Workspace",
	}, httptest.NewRequest(http.MethodPost, "/api/auth/register", nil))
	if err != nil {
		t.Fatalf("register owner: %v", err)
	}
	other, err := authService.Register(ctx, auth.RegisterRequest{
		Email:            "source-import-other-" + suffix + "@example.test",
		Username:         randomStorageSegment(),
		Password:         "Password123!",
		DisplayName:      "Source Import Other",
		OrganizationName: "Source Import Other Org " + suffix,
	}, httptest.NewRequest(http.MethodPost, "/api/auth/register", nil))
	if err != nil {
		t.Fatalf("register other: %v", err)
	}
	seed := &sourceImportSeed{
		ctx:                 ctx,
		pool:                pool,
		organizationID:      owner.OrganizationID,
		otherOrganizationID: other.OrganizationID,
		ownerToken:          owner.AccessToken,
		otherToken:          other.AccessToken,
	}
	var project Project
	doAPISuccess(t, server, http.MethodPost, "/api/projects", owner.AccessToken, owner.OrganizationID, map[string]any{
		"workspaceId": owner.WorkspaceID,
		"name":        "Source Import Project",
		"settings":    map[string]any{},
	}, &project)
	seed.projectID = project.ID
	return server, seed
}

func (s *sourceImportSeed) Close() {
	_, _ = s.pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, s.organizationID)
	_, _ = s.pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, s.otherOrganizationID)
	s.pool.Close()
}

func doMultipartAPISuccess[T any](t *testing.T, handler http.Handler, path, token, orgID string, fields map[string]string, fileName, fileContent string, target *T) {
	t.Helper()
	recorder := doMultipartAPIRequest(t, handler, path, token, orgID, fields, fileName, fileContent)
	if recorder.Code < 200 || recorder.Code >= 300 {
		t.Fatalf("POST %s status = %d body=%s", path, recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode success envelope: %v", err)
	}
	*target = envelope.Data
}

func doMultipartAPIRequest(t *testing.T, handler http.Handler, path, token, orgID string, fields map[string]string, fileName, fileContent string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatalf("write field %s: %v", key, err)
		}
	}
	part, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write([]byte(fileContent)); err != nil {
		t.Fatalf("write file content: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Organization-Id", orgID)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}
