package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/db"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/google/uuid"
)

func TestProjectVideoProductionProfileCreationIsAtomicAndRejectsReservedVersions(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run video production profile API tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for video production profile API tests")
	}
	t.Setenv(videoproduction.FeatureFlagEnvironmentVariable, "true")

	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	authService := auth.NewService(pool, "video-production-profile-test-secret", time.Hour, 24*time.Hour)
	handler := New(pool, authService, nil, nil, nil).Handler()
	suffix := uuid.NewString()
	registration, err := authService.Register(ctx, auth.RegisterRequest{
		Email:            "video-profile-" + suffix + "@example.test",
		Username:         randomStorageSegment(),
		Password:         "Password123!",
		DisplayName:      "Video Profile Test",
		OrganizationName: "Video Profile Org " + suffix,
		WorkspaceName:    "Video Profile Workspace",
	}, httptest.NewRequest(http.MethodPost, "/api/auth/register", nil))
	if err != nil {
		t.Fatalf("register test user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, registration.OrganizationID)
		pool.Close()
	})

	projectName := "Atomic Profile Project " + suffix
	var project Project
	doAPISuccess(t, handler, http.MethodPost, "/api/projects", registration.AccessToken, registration.OrganizationID, map[string]any{
		"workspaceId":                   registration.WorkspaceID,
		"name":                          projectName,
		"videoProductionProfileKey":     videoproduction.ProfileSingleFrameI2V,
		"videoProductionProfileVersion": 1,
		"compatibilityPolicy":           videoproduction.CompatibilityStrict,
	}, &project)
	if project.VideoProductionBinding == nil || project.ProductionGeneration == nil {
		t.Fatalf("project response is missing binding/generation: %#v", project)
	}
	if project.VideoProductionBinding.ProfileKey != videoproduction.ProfileSingleFrameI2V {
		t.Fatalf("profile key = %s", project.VideoProductionBinding.ProfileKey)
	}
	var projectCount, bindingCount, generationCount int
	if err := pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM projects WHERE id = $1),
			(SELECT count(*) FROM project_video_production_bindings WHERE id = $2 AND project_id = $1 AND status = 'active'),
			(SELECT count(*) FROM project_video_production_generations WHERE id = $3 AND project_id = $1 AND binding_id = $2 AND status = 'active')
	`, project.ID, project.VideoProductionBinding.ID, project.ProductionGeneration.ID).Scan(&projectCount, &bindingCount, &generationCount); err != nil {
		t.Fatalf("verify atomic project graph: %v", err)
	}
	if projectCount != 1 || bindingCount != 1 || generationCount != 1 {
		t.Fatalf("atomic graph counts = project:%d binding:%d generation:%d", projectCount, bindingCount, generationCount)
	}

	reservedName := "Reserved Profile Project " + suffix
	reservedResponse := doAPIRequest(t, handler, http.MethodPost, "/api/projects", registration.AccessToken, registration.OrganizationID, map[string]any{
		"workspaceId":                   registration.WorkspaceID,
		"name":                          reservedName,
		"videoProductionProfileKey":     videoproduction.ProfileFirstLastFrame,
		"videoProductionProfileVersion": 1,
	})
	assertVideoProductionAPIError(t, reservedResponse, http.StatusUnprocessableEntity, videoproduction.CodeProfileUnavailable)

	firstLastName := "First Last Profile Project " + suffix
	var firstLastProject Project
	doAPISuccess(t, handler, http.MethodPost, "/api/projects", registration.AccessToken, registration.OrganizationID, map[string]any{
		"workspaceId":                   registration.WorkspaceID,
		"name":                          firstLastName,
		"videoProductionProfileKey":     videoproduction.ProfileFirstLastFrame,
		"videoProductionProfileVersion": 2,
		"compatibilityPolicy":           videoproduction.CompatibilityStrict,
	}, &firstLastProject)
	if firstLastProject.VideoProductionBinding == nil || firstLastProject.ProductionGeneration == nil ||
		firstLastProject.VideoProductionBinding.ProfileKey != videoproduction.ProfileFirstLastFrame ||
		firstLastProject.VideoProductionBinding.ProfileVersion != 2 ||
		firstLastProject.VideoProductionBinding.ImplementationState != videoproduction.ImplementationAvailable {
		t.Fatalf("first/last profile binding = %#v", firstLastProject.VideoProductionBinding)
	}

	multimodalReserved := doAPIRequest(t, handler, http.MethodPost, "/api/projects", registration.AccessToken, registration.OrganizationID, map[string]any{
		"workspaceId":                   registration.WorkspaceID,
		"name":                          "Reserved Multimodal Profile Project " + suffix,
		"videoProductionProfileKey":     videoproduction.ProfileMultimodalReference,
		"videoProductionProfileVersion": 1,
	})
	assertVideoProductionAPIError(t, multimodalReserved, http.StatusUnprocessableEntity, videoproduction.CodeProfileUnavailable)

	multimodalName := "Multimodal Profile Project " + suffix
	var multimodalProject Project
	doAPISuccess(t, handler, http.MethodPost, "/api/projects", registration.AccessToken, registration.OrganizationID, map[string]any{
		"workspaceId":                   registration.WorkspaceID,
		"name":                          multimodalName,
		"videoProductionProfileKey":     videoproduction.ProfileMultimodalReference,
		"videoProductionProfileVersion": 2,
		"compatibilityPolicy":           videoproduction.CompatibilityStrict,
	}, &multimodalProject)
	if multimodalProject.VideoProductionBinding == nil || multimodalProject.ProductionGeneration == nil ||
		multimodalProject.VideoProductionBinding.ProfileKey != videoproduction.ProfileMultimodalReference ||
		multimodalProject.VideoProductionBinding.ProfileVersion != 2 ||
		multimodalProject.VideoProductionBinding.ImplementationState != videoproduction.ImplementationAvailable {
		t.Fatalf("multimodal profile binding = %#v", multimodalProject.VideoProductionBinding)
	}

	rollbackName := "Rollback Profile Project " + suffix
	rollbackResponse := doAPIRequest(t, handler, http.MethodPost, "/api/projects", registration.AccessToken, registration.OrganizationID, map[string]any{
		"workspaceId":                   registration.WorkspaceID,
		"name":                          rollbackName,
		"videoProductionProfileKey":     videoproduction.ProfileSingleFrameI2V,
		"directorManualPromptVersionId": uuid.NewString(),
	})
	if rollbackResponse.Code < 400 {
		t.Fatalf("expected project creation rollback, status=%d body=%s", rollbackResponse.Code, rollbackResponse.Body.String())
	}
	var leaked int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM projects WHERE organization_id = $1 AND name IN ($2, $3)
	`, registration.OrganizationID, reservedName, rollbackName).Scan(&leaked); err != nil {
		t.Fatalf("verify rollback: %v", err)
	}
	if leaked != 0 {
		t.Fatalf("failed project creation leaked %d project rows", leaked)
	}
}

func assertVideoProductionAPIError(t *testing.T, recorder *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if recorder.Code != status {
		t.Fatalf("status = %d, want %d body=%s", recorder.Code, status, recorder.Body.String())
	}
	var envelope httpx.Envelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if envelope.Error == nil || envelope.Error.Code != code {
		t.Fatalf("error = %#v, want %s", envelope.Error, code)
	}
}
