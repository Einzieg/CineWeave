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
