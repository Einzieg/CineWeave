package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/db"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/storage"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestArtifactPreviewSecurity(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	imageArtifactID := seed.insertArtifact(t, "generated_image", "org/project/image.png", "image/png")
	noStorageArtifactID := seed.insertArtifact(t, "storyboard_json", "", "application/json")
	unsupportedArtifactID := seed.insertArtifact(t, "binary", "org/project/binary.bin", "application/octet-stream")

	assertAPIErrorCode(t, server, http.MethodPost, "/api/artifacts/"+imageArtifactID+"/preview-url", seed.otherToken, seed.organizationID, map[string]any{"expiresSeconds": 900}, http.StatusForbidden, "FORBIDDEN")
	assertAPIErrorCode(t, server, http.MethodPost, "/api/artifacts/"+noStorageArtifactID+"/preview-url", seed.ownerToken, seed.organizationID, map[string]any{"expiresSeconds": 900}, http.StatusUnprocessableEntity, "ARTIFACT_HAS_NO_STORAGE_OBJECT")
	assertAPIErrorCode(t, server, http.MethodPost, "/api/artifacts/"+unsupportedArtifactID+"/preview-url", seed.ownerToken, seed.organizationID, map[string]any{"expiresSeconds": 900}, http.StatusUnprocessableEntity, "UNSUPPORTED_PREVIEW_TYPE")

	var preview struct {
		ArtifactID string    `json:"artifactId"`
		StorageKey string    `json:"storageKey"`
		URL        string    `json:"url"`
		Method     string    `json:"method"`
		ExpiresAt  time.Time `json:"expiresAt"`
	}
	doAPISuccess(t, server, http.MethodPost, "/api/artifacts/"+imageArtifactID+"/preview-url", seed.ownerToken, seed.organizationID, map[string]any{"expiresSeconds": 7200}, &preview)
	if preview.ArtifactID != imageArtifactID || preview.StorageKey == "" || preview.URL == "" || preview.Method != "GET" {
		t.Fatalf("preview response = %+v", preview)
	}
	if time.Until(preview.ExpiresAt) > time.Hour+5*time.Second {
		t.Fatalf("expiresAt was not clamped to one hour: %s", preview.ExpiresAt)
	}
	if !strings.Contains(preview.URL, "localhost:9000") {
		t.Fatalf("preview URL did not use public endpoint: %s", preview.URL)
	}
}

type artifactPreviewSeed struct {
	ctx                 context.Context
	pool                *pgxpool.Pool
	server              http.Handler
	authService         *auth.Service
	organizationID      string
	otherOrganizationID string
	ownerUserID         string
	ownerToken          string
	otherToken          string
	projectID           string
}

func setupArtifactPreviewTest(t *testing.T) (http.Handler, *artifactPreviewSeed) {
	t.Helper()
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run artifact preview API tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for artifact preview API tests")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	authService := auth.NewService(pool, "preview-test-secret", time.Hour, 24*time.Hour)
	storageClient, err := storage.New(ctx, storage.Config{
		Endpoint:        "http://minio:9000",
		PublicEndpoint:  "http://localhost:9000",
		Region:          "us-east-1",
		Bucket:          "cineweave",
		AccessKeyID:     "minio",
		SecretAccessKey: "minio123",
		UsePathStyle:    true,
	})
	if err != nil {
		t.Fatalf("create storage client: %v", err)
	}
	server := New(pool, authService, nil, storageClient, nil).Handler()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	ownerResp, err := authService.Register(ctx, auth.RegisterRequest{
		Email:            "preview-owner-" + suffix + "@example.test",
		Password:         "Password123!",
		DisplayName:      "Preview Owner",
		OrganizationName: "Preview Org " + suffix,
	}, httptest.NewRequest(http.MethodPost, "/api/auth/register", nil))
	if err != nil {
		t.Fatalf("register owner: %v", err)
	}
	otherResp, err := authService.Register(ctx, auth.RegisterRequest{
		Email:            "preview-other-" + suffix + "@example.test",
		Password:         "Password123!",
		DisplayName:      "Preview Other",
		OrganizationName: "Other Org " + suffix,
	}, httptest.NewRequest(http.MethodPost, "/api/auth/register", nil))
	if err != nil {
		t.Fatalf("register other: %v", err)
	}
	seed := &artifactPreviewSeed{
		ctx:                 ctx,
		pool:                pool,
		server:              server,
		authService:         authService,
		organizationID:      ownerResp.OrganizationID,
		otherOrganizationID: otherResp.OrganizationID,
		ownerUserID:         ownerResp.User.ID,
		ownerToken:          ownerResp.AccessToken,
		otherToken:          otherResp.AccessToken,
	}
	var workspaceID string
	if err := pool.QueryRow(ctx, `INSERT INTO workspaces(organization_id, name) VALUES ($1, 'Preview Workspace') RETURNING id`, seed.organizationID).Scan(&workspaceID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO projects(organization_id, workspace_id, name, created_by)
		VALUES ($1, $2, 'Preview Project', $3)
		RETURNING id
	`, seed.organizationID, workspaceID, seed.ownerUserID).Scan(&seed.projectID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO project_members(project_id, user_id) VALUES ($1, $2)`, seed.projectID, seed.ownerUserID); err != nil {
		t.Fatalf("insert project member: %v", err)
	}
	return server, seed
}

func (s *artifactPreviewSeed) Close() {
	_, _ = s.pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, s.organizationID)
	_, _ = s.pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, s.otherOrganizationID)
	s.pool.Close()
}

func (s *artifactPreviewSeed) insertArtifact(t *testing.T, artifactType, storageKey, mimeType string) string {
	t.Helper()
	var artifactID string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO artifacts(organization_id, project_id, type, storage_key, mime_type, metadata, created_by)
		VALUES ($1, $2, $3, $4, $5, '{}', $6)
		RETURNING id
	`, s.organizationID, s.projectID, artifactType, nullableString(storageKey), nullableString(mimeType), s.ownerUserID).Scan(&artifactID); err != nil {
		t.Fatalf("insert artifact: %v", err)
	}
	return artifactID
}

func assertAPIErrorCode(t *testing.T, handler http.Handler, method, path, token, orgID string, body any, status int, code string) {
	t.Helper()
	recorder := doAPIRequest(t, handler, method, path, token, orgID, body)
	if recorder.Code != status {
		t.Fatalf("%s %s status = %d, want %d body=%s", method, path, recorder.Code, status, recorder.Body.String())
	}
	var envelope httpx.Envelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if envelope.Error == nil || envelope.Error.Code != code {
		t.Fatalf("error code = %#v, want %s", envelope.Error, code)
	}
}

func doAPISuccess[T any](t *testing.T, handler http.Handler, method, path, token, orgID string, body any, target *T) {
	t.Helper()
	recorder := doAPIRequest(t, handler, method, path, token, orgID, body)
	if recorder.Code < 200 || recorder.Code >= 300 {
		t.Fatalf("%s %s status = %d body=%s", method, path, recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode success envelope: %v", err)
	}
	*target = envelope.Data
}

func doAPIRequest(t *testing.T, handler http.Handler, method, path, token, orgID string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var requestBody *bytes.Reader
	if body == nil {
		requestBody = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		requestBody = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, requestBody)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Organization-Id", orgID)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}
