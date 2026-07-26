package api

import (
	"bytes"
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
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"
)

type projectDeletionTemporalStub struct{}

func (projectDeletionTemporalStub) ExecuteWorkflow(
	context.Context,
	client.StartWorkflowOptions,
	interface{},
	...interface{},
) (client.WorkflowRun, error) {
	return nil, nil
}

func (projectDeletionTemporalStub) CancelWorkflow(context.Context, string, string) error {
	return nil
}

func (projectDeletionTemporalStub) SignalWorkflow(context.Context, string, string, string, interface{}) error {
	return nil
}

func TestProjectDeletionAPILifecycleIntegration(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run project deletion API tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for project deletion API tests")
	}

	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	require.NoError(t, err)
	authService := auth.NewService(pool, "project-deletion-test-secret", time.Hour, 24*time.Hour)
	apiServer := New(pool, authService, nil, nil, nil)
	apiServer.temporal = projectDeletionTemporalStub{}
	handler := apiServer.Handler()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	registration, err := authService.Register(ctx, auth.RegisterRequest{
		Email:            "project-deletion-owner-" + suffix + "@example.test",
		Username:         "delete_owner_" + suffix[:12],
		Password:         "Password123!",
		DisplayName:      "Project Deletion Owner",
		OrganizationName: "Project Deletion Org " + suffix,
		WorkspaceName:    "Project Deletion Workspace",
	}, httptest.NewRequest(http.MethodPost, "/api/auth/register", nil))
	require.NoError(t, err)
	userIDs := []string{registration.User.ID}
	_, err = pool.Exec(ctx, `UPDATE users SET is_system_admin = true WHERE id = $1`, registration.User.ID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, registration.OrganizationID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = ANY($1::uuid[])`, userIDs)
		pool.Close()
	})

	var projectID string
	const projectName = "待删除项目"
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO projects(
			organization_id, workspace_id, name, created_by, video_production_state
		)
		VALUES ($1, $2, $3, $4, 'unconfigured')
		RETURNING id::text
	`, registration.OrganizationID, registration.WorkspaceID, projectName, registration.User.ID).Scan(&projectID))
	_, err = pool.Exec(ctx, `INSERT INTO project_members(project_id, user_id) VALUES ($1, $2)`, projectID, registration.User.ID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO artifacts(organization_id, project_id, type, storage_key, mime_type, metadata, created_by)
		VALUES ($1, $2, 'storyboard_image', $3, 'image/png', '{}'::jsonb, $4)
	`, registration.OrganizationID, projectID, "projects/"+projectID+"/storyboard.png", registration.User.ID)
	require.NoError(t, err)

	member, err := authService.CreateSystemOrganizationMember(
		ctx,
		registration.User.ID,
		registration.OrganizationID,
		auth.CreateSystemOrganizationMemberRequest{
			Email:       "project-deletion-member-" + suffix + "@example.test",
			Username:    "delete_member_" + suffix[:12],
			Password:    "Password123!",
			DisplayName: "Project Deletion Member",
		},
	)
	require.NoError(t, err)
	userIDs = append(userIDs, member.User.ID)
	memberLogin, err := authService.Login(ctx, auth.LoginRequest{
		Identifier: member.User.Username,
		Password:   "Password123!",
	}, httptest.NewRequest(http.MethodPost, "/api/auth/login", nil))
	require.NoError(t, err)
	require.NotNil(t, memberLogin.TokenResponse)

	unauthorized := projectDeletionAPIRequest(
		t,
		handler,
		http.MethodGet,
		"/api/projects/"+projectID+"/deletion-impact",
		memberLogin.AccessToken,
		registration.OrganizationID,
		"",
		nil,
	)
	require.Equal(t, http.StatusForbidden, unauthorized.Code, unauthorized.Body.String())

	impactResponse := projectDeletionAPIRequest(
		t,
		handler,
		http.MethodGet,
		"/api/projects/"+projectID+"/deletion-impact",
		registration.AccessToken,
		registration.OrganizationID,
		"",
		nil,
	)
	require.Equal(t, http.StatusOK, impactResponse.Code, impactResponse.Body.String())
	var impactEnvelope struct {
		Data ProjectDeletionImpact `json:"data"`
	}
	require.NoError(t, json.Unmarshal(impactResponse.Body.Bytes(), &impactEnvelope))
	require.Equal(t, projectID, impactEnvelope.Data.ProjectID)
	require.Equal(t, 1, impactEnvelope.Data.ArtifactCount)
	require.Equal(t, 1, impactEnvelope.Data.StorageObjectCount)
	require.Len(t, impactEnvelope.Data.ImpactHash, 64)

	wrongName := projectDeletionAPIRequest(
		t,
		handler,
		http.MethodPost,
		"/api/projects/"+projectID+"/deletion-requests",
		registration.AccessToken,
		registration.OrganizationID,
		"delete-wrong-name-"+suffix,
		map[string]any{
			"projectName":             "错误名称",
			"expectedProjectRevision": impactEnvelope.Data.ProjectRevision,
			"impactHash":              impactEnvelope.Data.ImpactHash,
		},
	)
	require.Equal(t, http.StatusUnprocessableEntity, wrongName.Code, wrongName.Body.String())
	require.Contains(t, wrongName.Body.String(), "PROJECT_NAME_CONFIRMATION_MISMATCH")

	idempotencyKey := "delete-project-" + suffix
	createBody := map[string]any{
		"projectName":             projectName,
		"expectedProjectRevision": impactEnvelope.Data.ProjectRevision,
		"impactHash":              impactEnvelope.Data.ImpactHash,
	}
	created := projectDeletionAPIRequest(
		t,
		handler,
		http.MethodPost,
		"/api/projects/"+projectID+"/deletion-requests",
		registration.AccessToken,
		registration.OrganizationID,
		idempotencyKey,
		createBody,
	)
	require.Equal(t, http.StatusAccepted, created.Code, created.Body.String())
	var createdEnvelope struct {
		Data ProjectDeletionRequest `json:"data"`
	}
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &createdEnvelope))
	require.Equal(t, "requested", createdEnvelope.Data.Status)
	require.Equal(t, int64(1), createdEnvelope.Data.DeletionRevision)

	replayed := projectDeletionAPIRequest(
		t,
		handler,
		http.MethodPost,
		"/api/projects/"+projectID+"/deletion-requests",
		registration.AccessToken,
		registration.OrganizationID,
		idempotencyKey,
		createBody,
	)
	require.Equal(t, http.StatusAccepted, replayed.Code, replayed.Body.String())
	var replayedEnvelope struct {
		Data ProjectDeletionRequest `json:"data"`
	}
	require.NoError(t, json.Unmarshal(replayed.Body.Bytes(), &replayedEnvelope))
	require.Equal(t, createdEnvelope.Data.ID, replayedEnvelope.Data.ID)

	conflict := projectDeletionAPIRequest(
		t,
		handler,
		http.MethodPost,
		"/api/projects/"+projectID+"/deletion-requests",
		registration.AccessToken,
		registration.OrganizationID,
		"delete-project-conflict-"+suffix,
		createBody,
	)
	require.Equal(t, http.StatusConflict, conflict.Code, conflict.Body.String())
	require.Contains(t, conflict.Body.String(), codeProjectDeletionAlreadyRunning)

	listed := projectDeletionAPIRequest(
		t,
		handler,
		http.MethodGet,
		"/api/projects?filter[workspaceId]="+registration.WorkspaceID,
		registration.AccessToken,
		registration.OrganizationID,
		"",
		nil,
	)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	var listEnvelope struct {
		Data struct {
			Items []Project `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(listed.Body.Bytes(), &listEnvelope))
	for _, item := range listEnvelope.Data.Items {
		require.NotEqual(t, projectID, item.ID, "deleting project must be hidden from the default list")
	}

	_, err = pool.Exec(ctx, `
		UPDATE project_deletion_requests
		SET status = 'failed_retryable',
		    error_code = 'PROJECT_DELETION_STORAGE_FAILED',
		    error_message = 'temporary storage failure'
		WHERE id = $1
	`, createdEnvelope.Data.ID)
	require.NoError(t, err)
	retried := projectDeletionAPIRequest(
		t,
		handler,
		http.MethodPost,
		"/api/projects/"+projectID+"/deletion-requests/"+createdEnvelope.Data.ID+"/retry",
		registration.AccessToken,
		registration.OrganizationID,
		"",
		nil,
	)
	require.Equal(t, http.StatusAccepted, retried.Code, retried.Body.String())
	var retriedEnvelope struct {
		Data ProjectDeletionRequest `json:"data"`
	}
	require.NoError(t, json.Unmarshal(retried.Body.Bytes(), &retriedEnvelope))
	require.Equal(t, "requested", retriedEnvelope.Data.Status)
	require.Equal(t, 1, retriedEnvelope.Data.RetryCount)
	require.Nil(t, retriedEnvelope.Data.ErrorCode)
	require.Nil(t, retriedEnvelope.Data.ErrorMessage)
}

func projectDeletionAPIRequest(
	t *testing.T,
	handler http.Handler,
	method string,
	path string,
	accessToken string,
	organizationID string,
	idempotencyKey string,
	body any,
) *httptest.ResponseRecorder {
	t.Helper()
	var payload []byte
	var err error
	if body != nil {
		payload, err = json.Marshal(body)
		require.NoError(t, err)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(payload))
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("X-Organization-Id", organizationID)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
