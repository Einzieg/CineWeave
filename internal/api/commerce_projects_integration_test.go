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
	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/db"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/Einzieg/cineweave/internal/workflows"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
)

func TestCommerceProjectOptionsRequireProjectWrite(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run commerce project API tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for commerce project API tests")
	}

	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	require.NoError(t, err)
	authService := auth.NewService(pool, "commerce-project-options-test-secret", time.Hour, 24*time.Hour)
	handler := New(pool, authService, nil, nil, nil).Handler()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	registration, err := authService.Setup(ctx, auth.RegisterRequest{
		Email:            "commerce-options-owner-" + suffix + "@example.test",
		Username:         "commerce_owner_" + suffix[:12],
		Password:         "Password123!",
		DisplayName:      "Commerce Options Owner",
		OrganizationName: "Commerce Options Org " + suffix,
		WorkspaceName:    "Commerce Options Workspace",
	}, httptest.NewRequest(http.MethodPost, "/api/auth/register", nil))
	require.NoError(t, err)
	userIDs := []string{registration.User.ID}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, registration.OrganizationID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = ANY($1::uuid[])`, userIDs)
		pool.Close()
	})

	seedCommerceWorkflowTemplate(t, ctx, pool, registration.OrganizationID, registration.User.ID)
	ownerRequest := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+registration.WorkspaceID+"/commerce/project-options", nil)
	ownerRequest.Header.Set("Authorization", "Bearer "+registration.AccessToken)
	ownerRequest.Header.Set("X-Organization-Id", registration.OrganizationID)
	ownerResponse := httptest.NewRecorder()
	handler.ServeHTTP(ownerResponse, ownerRequest)
	require.Equal(t, http.StatusOK, ownerResponse.Code, ownerResponse.Body.String())
	var ownerEnvelope struct {
		Data commercepkg.ProjectOptions `json:"data"`
	}
	require.NoError(t, json.Unmarshal(ownerResponse.Body.Bytes(), &ownerEnvelope))
	require.NotEmpty(t, ownerEnvelope.Data.WorkflowTemplateVersionID)

	member, err := authService.CreateSystemOrganizationMember(ctx, registration.User.ID, registration.OrganizationID, auth.CreateSystemOrganizationMemberRequest{
		Email:       "commerce-options-member-" + suffix + "@example.test",
		Username:    "commerce_member_" + suffix[:12],
		Password:    "Password123!",
		DisplayName: "Commerce Read Only Member",
	})
	require.NoError(t, err)
	userIDs = append(userIDs, member.User.ID)
	memberLogin, err := authService.Login(ctx, auth.LoginRequest{
		Identifier: member.User.Username,
		Password:   "Password123!",
	}, httptest.NewRequest(http.MethodPost, "/api/auth/login", nil))
	require.NoError(t, err)
	require.NotNil(t, memberLogin.TokenResponse)
	memberRequest := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+registration.WorkspaceID+"/commerce/project-options", nil)
	memberRequest.Header.Set("Authorization", "Bearer "+memberLogin.AccessToken)
	memberRequest.Header.Set("X-Organization-Id", registration.OrganizationID)
	memberResponse := httptest.NewRecorder()
	handler.ServeHTTP(memberResponse, memberRequest)
	assertCommerceProjectError(t, memberResponse, http.StatusForbidden, "ACCESS_DENIED")
}

func TestCommerceAgentToolsRequireEveryDeclaredPermission(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run commerce project API tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for commerce project API tests")
	}

	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	require.NoError(t, err)
	authService := auth.NewService(pool, "commerce-agent-permissions-test-secret", time.Hour, 24*time.Hour)
	vault, err := provider.NewVault("")
	require.NoError(t, err)
	handler := New(pool, authService, provider.NewService(pool, vault), nil, nil).Handler()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	registration, err := authService.Setup(ctx, auth.RegisterRequest{
		Email:            "commerce-agent-owner-" + suffix + "@example.test",
		Username:         "comm_agent_owner_" + suffix[:12],
		Password:         "Password123!",
		DisplayName:      "Commerce Agent Owner",
		OrganizationName: "Commerce Agent Org " + suffix,
		WorkspaceName:    "Commerce Agent Workspace",
	}, httptest.NewRequest(http.MethodPost, "/api/auth/register", nil))
	require.NoError(t, err)
	userIDs := []string{registration.User.ID}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, registration.OrganizationID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = ANY($1::uuid[])`, userIDs)
		pool.Close()
	})

	seedCommerceWorkflowTemplate(t, ctx, pool, registration.OrganizationID, registration.User.ID)
	_, _ = seedCommerceDirectVideoModel(t, ctx, pool, registration.OrganizationID, registration.User.ID)
	createResponse := doCommerceProjectCreateRequest(t, handler, registration.AccessToken, registration.OrganizationID, "commerce-agent-permissions-"+suffix, map[string]any{
		"workspaceId":                  registration.WorkspaceID,
		"name":                         "Commerce Agent Permissions " + suffix,
		"projectKind":                  "commerce_video",
		"videoRatio":                   "9:16",
		"defaultTargetDurationSeconds": 16,
		"defaultTargetPlatform":        "douyin",
		"defaultLanguageMode":          "auto",
	})
	require.Equal(t, http.StatusCreated, createResponse.Code, createResponse.Body.String())
	project := decodeCommerceProjectEnvelope(t, createResponse).Data

	createMember := func(label string) auth.TokenResponse {
		t.Helper()
		member, createErr := authService.CreateSystemOrganizationMember(ctx, registration.User.ID, registration.OrganizationID, auth.CreateSystemOrganizationMemberRequest{
			Email:       "commerce-agent-" + label + "-" + suffix + "@example.test",
			Username:    "commerce_agent_" + label + "_" + suffix[:8],
			Password:    "Password123!",
			DisplayName: "Commerce Agent " + label,
		})
		require.NoError(t, createErr)
		userIDs = append(userIDs, member.User.ID)
		login, loginErr := authService.Login(ctx, auth.LoginRequest{
			Identifier: member.User.Username,
			Password:   "Password123!",
		}, httptest.NewRequest(http.MethodPost, "/api/auth/login", nil))
		require.NoError(t, loginErr)
		require.NotNil(t, login.TokenResponse)
		return *login.TokenResponse
	}
	bindPermissions := func(member auth.TokenResponse, roleKey string, permissions []string) {
		t.Helper()
		var role Role
		doAPISuccess(t, handler, http.MethodPost, "/api/roles", registration.AccessToken, registration.OrganizationID, map[string]any{
			"roleKey":        roleKey,
			"name":           roleKey,
			"scope":          "project",
			"permissionKeys": permissions,
		}, &role)
		var binding RoleBinding
		doAPISuccess(t, handler, http.MethodPost, "/api/role-bindings", registration.AccessToken, registration.OrganizationID, map[string]any{
			"organizationId":    registration.OrganizationID,
			"roleId":            role.ID,
			"subjectType":       "user",
			"subjectUserId":     member.User.ID,
			"resourceType":      "project",
			"resourceProjectId": project.ID,
		}, &binding)
	}
	listToolNames := func(member auth.TokenResponse) map[string]bool {
		t.Helper()
		var tools struct {
			Items []struct {
				Name string `json:"name"`
			} `json:"items"`
		}
		doAPISuccess(t, handler, http.MethodGet, "/api/projects/"+project.ID+"/agent/tools", member.AccessToken, registration.OrganizationID, nil, &tools)
		names := make(map[string]bool, len(tools.Items))
		for _, tool := range tools.Items {
			names[tool.Name] = true
		}
		return names
	}

	scriptOnly := createMember("script")
	bindPermissions(scriptOnly, "commerce_script_only_"+suffix[:8], []string{"project.read", "script.read", "script.write"})
	scriptOnlyTools := listToolNames(scriptOnly)
	require.True(t, scriptOnlyTools["commerce.script.create"])
	require.False(t, scriptOnlyTools["commerce.script.derive.batch"])
	require.False(t, scriptOnlyTools["commerce.script.derive.retry_failed"])
	assertAPIErrorCode(
		t,
		handler,
		http.MethodPost,
		"/api/projects/"+project.ID+"/commerce/script-units/"+uuid.NewString()+"/derivations",
		scriptOnly.AccessToken,
		registration.OrganizationID,
		map[string]any{"dimension": "scene", "instruction": "five variants", "variations": []map[string]any{{"label": "one"}}},
		http.StatusForbidden,
		"ACCESS_DENIED",
	)

	runWithoutCancel := createMember("runner")
	bindPermissions(runWithoutCancel, "commerce_runner_"+suffix[:8], []string{"project.read", "script.read", "script.write", "workflow.run"})
	runnerTools := listToolNames(runWithoutCancel)
	require.True(t, runnerTools["commerce.script.derive.batch"])
	require.True(t, runnerTools["commerce.video.generate"])
	require.False(t, runnerTools["commerce.script.derive.cancel"])
	require.False(t, runnerTools["commerce.video.cancel"])
	assertAPIErrorCode(
		t,
		handler,
		http.MethodPost,
		"/api/projects/"+project.ID+"/commerce/direct-videos/"+uuid.NewString()+"/cancel",
		runWithoutCancel.AccessToken,
		registration.OrganizationID,
		nil,
		http.StatusForbidden,
		"ACCESS_DENIED",
	)
}

func TestCreateCommerceProjectIsDurablyIdempotent(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run commerce project API tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for commerce project API tests")
	}

	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	require.NoError(t, err)
	authService := auth.NewService(pool, "commerce-project-test-secret", time.Hour, 24*time.Hour)
	vault, err := provider.NewVault("")
	require.NoError(t, err)
	handler := New(pool, authService, provider.NewService(pool, vault), nil, nil).Handler()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	registration, err := authService.Setup(ctx, auth.RegisterRequest{
		Email:            "commerce-project-" + suffix + "@example.test",
		Username:         "commerce_" + suffix[:16],
		Password:         "Password123!",
		DisplayName:      "Commerce Project Test",
		OrganizationName: "Commerce Project Org " + suffix,
		WorkspaceName:    "Commerce Project Workspace",
	}, httptest.NewRequest(http.MethodPost, "/api/auth/register", nil))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, registration.OrganizationID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, registration.User.ID)
		pool.Close()
	})

	seedCommerceWorkflowTemplate(t, ctx, pool, registration.OrganizationID, registration.User.ID)
	providerAccountID, providerModelID := seedCommerceDirectVideoModel(
		t, ctx, pool, registration.OrganizationID, registration.User.ID,
	)
	optionsRequest := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+registration.WorkspaceID+"/commerce/project-options", nil)
	optionsRequest.Header.Set("Authorization", "Bearer "+registration.AccessToken)
	optionsRequest.Header.Set("X-Organization-Id", registration.OrganizationID)
	optionsResponse := httptest.NewRecorder()
	handler.ServeHTTP(optionsResponse, optionsRequest)
	require.Equal(t, http.StatusOK, optionsResponse.Code, optionsResponse.Body.String())
	var optionsEnvelope struct {
		Data commercepkg.ProjectOptions `json:"data"`
	}
	require.NoError(t, json.Unmarshal(optionsResponse.Body.Bytes(), &optionsEnvelope))
	require.Equal(t, []int{6, 10, 12, 16}, optionsEnvelope.Data.Durations)
	require.Len(t, optionsEnvelope.Data.Languages, 2)
	require.Len(t, optionsEnvelope.Data.ModelRequirements, 4)
	require.True(t, optionsEnvelope.Data.Available, "configured video model should make direct project creation available")
	require.Empty(t, optionsEnvelope.Data.Blockers)

	key := "commerce-create-" + suffix
	body := map[string]any{
		"workspaceId":                  registration.WorkspaceID,
		"name":                         "多语言商品视频 " + suffix,
		"projectKind":                  "commerce_video",
		"videoRatio":                   "9:16",
		"defaultTargetDurationSeconds": 16,
		"defaultTargetPlatform":        "douyin",
		"defaultLanguageMode":          "explicit",
		"defaultTargetLanguage":        "zh-CN",
	}

	first := doCommerceProjectCreateRequest(t, handler, registration.AccessToken, registration.OrganizationID, key, body)
	require.Equal(t, http.StatusCreated, first.Code, first.Body.String())
	firstEnvelope := decodeCommerceProjectEnvelope(t, first)
	require.Equal(t, commercepkg.ProjectKindCommerceVideo, firstEnvelope.Data.ProjectKind)
	require.NotNil(t, firstEnvelope.Data.SetupSessionID)
	require.NotNil(t, firstEnvelope.Data.WorkflowTemplateVersionID)
	require.Len(t, dereferenceString(firstEnvelope.Data.SetupConfigurationHash), 64)
	require.Equal(t, &commercepkg.ScriptUnitDefaults{
		TargetDurationSeconds: 16,
		TargetPlatform:        "douyin",
		LanguageMode:          "explicit",
		TargetLanguage:        stringPointer("zh-CN"),
	}, firstEnvelope.Data.ScriptUnitDefaults)
	require.Nil(t, firstEnvelope.Meta)

	replay := doCommerceProjectCreateRequest(t, handler, registration.AccessToken, registration.OrganizationID, key, body)
	require.Equal(t, http.StatusCreated, replay.Code, replay.Body.String())
	replayEnvelope := decodeCommerceProjectEnvelope(t, replay)
	require.Equal(t, firstEnvelope.Data.ID, replayEnvelope.Data.ID)
	require.Equal(t, firstEnvelope.Data.SetupSessionID, replayEnvelope.Data.SetupSessionID)
	require.Equal(t, true, replayEnvelope.Meta["idempotentReplay"])

	getRequest := httptest.NewRequest(http.MethodGet, "/api/projects/"+firstEnvelope.Data.ID, nil)
	getRequest.Header.Set("Authorization", "Bearer "+registration.AccessToken)
	getRequest.Header.Set("X-Organization-Id", registration.OrganizationID)
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, getRequest)
	require.Equal(t, http.StatusOK, getResponse.Code, getResponse.Body.String())
	getEnvelope := decodeCommerceProjectEnvelope(t, getResponse)
	require.Equal(t, firstEnvelope.Data.SetupSessionID, getEnvelope.Data.SetupSessionID)
	require.Equal(t, firstEnvelope.Data.SetupState, getEnvelope.Data.SetupState)
	require.Equal(t, firstEnvelope.Data.WorkflowTemplateVersionID, getEnvelope.Data.WorkflowTemplateVersionID)
	require.Equal(t, firstEnvelope.Data.SetupConfigurationHash, getEnvelope.Data.SetupConfigurationHash)

	conflictingBody := cloneCommerceCreateBody(body)
	conflictingBody["name"] = "冲突商品视频 " + suffix
	conflict := doCommerceProjectCreateRequest(t, handler, registration.AccessToken, registration.OrganizationID, key, conflictingBody)
	assertCommerceProjectError(t, conflict, http.StatusConflict, "IDEMPOTENCY_KEY_CONFLICT")

	missing := doCommerceProjectCreateRequest(t, handler, registration.AccessToken, registration.OrganizationID, "", body)
	assertCommerceProjectError(t, missing, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REQUIRED")

	var projectCount, setupCount, idempotencyCount int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM projects WHERE organization_id = $1 AND project_kind = 'commerce_video'),
			(SELECT count(*) FROM commerce_setup_sessions WHERE organization_id = $1 AND idempotency_scope = $2 AND client_request_id = $3),
			(SELECT count(*) FROM idempotency_keys WHERE organization_id = $1 AND scope = $2 AND key = $3)
	`, registration.OrganizationID, commerceProjectCreateScope, key).Scan(&projectCount, &setupCount, &idempotencyCount))
	require.Equal(t, 1, projectCount)
	require.Equal(t, 1, setupCount)
	require.Equal(t, 1, idempotencyCount)
	var productID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO commerce_products(organization_id, project_id, status, created_by)
		VALUES ($1, $2, 'draft', $3)
		RETURNING id::text
	`, registration.OrganizationID, firstEnvelope.Data.ID, registration.User.ID).Scan(&productID))
	var productVersionID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO commerce_product_versions(
			organization_id, project_id, product_id, version, name,
			selling_points, immutable_features, prohibited_claims,
			facts_snapshot, facts_hash, created_by
		)
		VALUES ($1, $2, $3, 1, '直出测试商品', '[]', '{}', '[]',
		        '{"name":"直出测试商品"}', $4, $5)
		RETURNING id::text
	`, registration.OrganizationID, firstEnvelope.Data.ID, productID,
		strings.Repeat("d", 64), registration.User.ID).Scan(&productVersionID))
	_, err = pool.Exec(ctx, `
		UPDATE commerce_products
		SET current_version_id = $2, status = 'ready'
		WHERE id = $1
	`, productID, productVersionID)
	require.NoError(t, err)
	var productArtifactID, productMediaFileID, productReferenceID string
	productStorageKey := "commerce/direct-video-" + suffix + ".png"
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO artifacts(
			organization_id, project_id, type, storage_key, mime_type, metadata, created_by
		)
		VALUES ($1, $2, 'commerce_product_reference', $3, 'image/png', '{}', $4)
		RETURNING id::text
	`, registration.OrganizationID, firstEnvelope.Data.ID, productStorageKey,
		registration.User.ID).Scan(&productArtifactID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO media_files(
			organization_id, project_id, artifact_id, storage_key, mime_type,
			byte_size, width, height, checksum, metadata, created_by
		)
		VALUES ($1, $2, $3, $4, 'image/png', 128, 720, 1280, $5, '{}', $6)
		RETURNING id::text
	`, registration.OrganizationID, firstEnvelope.Data.ID, productArtifactID,
		productStorageKey, strings.Repeat("e", 64), registration.User.ID).Scan(&productMediaFileID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO commerce_product_references(
			organization_id, project_id, product_id, artifact_id, media_file_id,
			reference_role, ordinal, is_primary, width, height, mime_type,
			content_hash, quality_review, created_by
		)
		VALUES ($1, $2, $3, $4, $5, 'primary', 0, true, 720, 1280,
		        'image/png', $6, '{}', $7)
		RETURNING id::text
	`, registration.OrganizationID, firstEnvelope.Data.ID, productID,
		productArtifactID, productMediaFileID, strings.Repeat("e", 64),
		registration.User.ID).Scan(&productReferenceID))

	projectStatusRequest := httptest.NewRequest(http.MethodGet, "/api/projects/"+firstEnvelope.Data.ID+"/commerce/production-status", nil)
	projectStatusRequest.Header.Set("Authorization", "Bearer "+registration.AccessToken)
	projectStatusRequest.Header.Set("X-Organization-Id", registration.OrganizationID)
	projectStatusResponse := httptest.NewRecorder()
	handler.ServeHTTP(projectStatusResponse, projectStatusRequest)
	require.Equal(t, http.StatusOK, projectStatusResponse.Code, projectStatusResponse.Body.String())
	var projectStatusEnvelope struct {
		Data commerceProjectProductionStatus `json:"data"`
	}
	require.NoError(t, json.Unmarshal(projectStatusResponse.Body.Bytes(), &projectStatusEnvelope))
	require.Equal(t, 0, projectStatusEnvelope.Data.Overall.ScriptUnitCount)
	require.Equal(t, int64(1), projectStatusEnvelope.Data.ScriptUnitsRevision)

	unitBody, err := json.Marshal(map[string]any{
		"expectedScriptUnitsRevision": 1,
		"title":                       "首条带货脚本",
		"content":                     "展示产品卖点并引导下单。",
		"languageMode":                "explicit",
		"explicitTargetLanguage":      "zh-CN",
		"targetDurationSeconds":       6,
		"targetPlatform":              "douyin",
	})
	require.NoError(t, err)
	unitRequest := httptest.NewRequest(http.MethodPost, "/api/projects/"+firstEnvelope.Data.ID+"/commerce/script-units", bytes.NewReader(unitBody))
	unitRequest.Header.Set("Content-Type", "application/json")
	unitRequest.Header.Set("Authorization", "Bearer "+registration.AccessToken)
	unitRequest.Header.Set("X-Organization-Id", registration.OrganizationID)
	unitResponse := httptest.NewRecorder()
	handler.ServeHTTP(unitResponse, unitRequest)
	require.Equal(t, http.StatusCreated, unitResponse.Code, unitResponse.Body.String())
	var unitEnvelope struct {
		Data commercepkg.ScriptVersionMutation `json:"data"`
	}
	require.NoError(t, json.Unmarshal(unitResponse.Body.Bytes(), &unitEnvelope))
	require.NotEmpty(t, unitEnvelope.Data.ScriptUnit.ID)
	require.Equal(t, "draft", unitEnvelope.Data.ScriptUnit.Status)

	directOptionsRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/projects/"+firstEnvelope.Data.ID+"/commerce/video-options",
		nil,
	)
	directOptionsRequest.Header.Set("Authorization", "Bearer "+registration.AccessToken)
	directOptionsRequest.Header.Set("X-Organization-Id", registration.OrganizationID)
	directOptionsResponse := httptest.NewRecorder()
	handler.ServeHTTP(directOptionsResponse, directOptionsRequest)
	require.Equal(t, http.StatusOK, directOptionsResponse.Code, directOptionsResponse.Body.String())
	var directOptionsEnvelope struct {
		Data commercepkg.DirectVideoOptions `json:"data"`
	}
	require.NoError(t, json.Unmarshal(directOptionsResponse.Body.Bytes(), &directOptionsEnvelope))
	require.Equal(t, []int{6, 10, 12, 16}, directOptionsEnvelope.Data.ExecutableDurationSeconds)
	require.Equal(t, "720p", directOptionsEnvelope.Data.DefaultResolution)

	directVideoBody, err := json.Marshal(map[string]any{
		"durationSeconds": 6,
		"resolution":      "720p",
		"aspectRatio":     "9:16",
		"generateAudio":   true,
		"references": []map[string]any{{
			"sourceType": "product",
			"sourceId":   productReferenceID,
		}},
	})
	require.NoError(t, err)
	directVideoRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/projects/"+firstEnvelope.Data.ID+"/commerce/script-units/"+
			unitEnvelope.Data.ScriptUnit.ID+"/direct-videos",
		bytes.NewReader(directVideoBody),
	)
	directVideoRequest.Header.Set("Content-Type", "application/json")
	directVideoRequest.Header.Set("Authorization", "Bearer "+registration.AccessToken)
	directVideoRequest.Header.Set("X-Organization-Id", registration.OrganizationID)
	directVideoRequest.Header.Set("Idempotency-Key", "commerce-direct-video-"+suffix)
	directVideoResponse := httptest.NewRecorder()
	handler.ServeHTTP(directVideoResponse, directVideoRequest)
	require.Equal(t, http.StatusAccepted, directVideoResponse.Code, directVideoResponse.Body.String())
	var directVideoEnvelope struct {
		Data commercepkg.DirectVideoJob `json:"data"`
	}
	require.NoError(t, json.Unmarshal(directVideoResponse.Body.Bytes(), &directVideoEnvelope))
	require.Equal(t, "queued", directVideoEnvelope.Data.Status)
	require.Equal(t, 6, directVideoEnvelope.Data.RequestedDurationSeconds)
	require.Equal(t, "展示产品卖点并引导下单。", directVideoEnvelope.Data.ScriptSnapshot)
	require.Len(t, directVideoEnvelope.Data.References, 1)
	require.Equal(t, "first_frame", directVideoEnvelope.Data.References[0].ReferenceRole)
	require.Equal(t, productReferenceID, directVideoEnvelope.Data.References[0].SourceID)
	var directWorkflowCount, directOutboxCount int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM workflow_runs
			 WHERE id = $1 AND workflow_type = 'commerce_direct_video' AND status = 'queued'),
			(SELECT count(*) FROM workflow_start_outbox
			 WHERE workflow_run_id = $1 AND workflow_type = 'commerce_direct_video')
	`, *directVideoEnvelope.Data.WorkflowRunID).Scan(&directWorkflowCount, &directOutboxCount))
	require.Equal(t, 1, directWorkflowCount)
	require.Equal(t, 1, directOutboxCount)
	missingCancelKeyRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/projects/"+firstEnvelope.Data.ID+"/commerce/direct-videos/"+
			directVideoEnvelope.Data.ID+"/cancel",
		bytes.NewBufferString(`{"reason":"集成测试取消"}`),
	)
	missingCancelKeyRequest.Header.Set("Content-Type", "application/json")
	missingCancelKeyRequest.Header.Set("Authorization", "Bearer "+registration.AccessToken)
	missingCancelKeyRequest.Header.Set("X-Organization-Id", registration.OrganizationID)
	missingCancelKeyResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingCancelKeyResponse, missingCancelKeyRequest)
	assertCommerceProjectError(
		t, missingCancelKeyResponse, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REQUIRED",
	)

	cancelBody := bytes.NewBufferString(`{"reason":"集成测试取消"}`)
	cancelRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/projects/"+firstEnvelope.Data.ID+"/commerce/direct-videos/"+
			directVideoEnvelope.Data.ID+"/cancel",
		cancelBody,
	)
	cancelRequest.Header.Set("Content-Type", "application/json")
	cancelRequest.Header.Set("Authorization", "Bearer "+registration.AccessToken)
	cancelRequest.Header.Set("X-Organization-Id", registration.OrganizationID)
	cancelRequest.Header.Set("Idempotency-Key", "commerce-direct-video-cancel-"+suffix)
	cancelResponse := httptest.NewRecorder()
	handler.ServeHTTP(cancelResponse, cancelRequest)
	require.Equal(t, http.StatusAccepted, cancelResponse.Code, cancelResponse.Body.String())
	var cancelledEnvelope struct {
		Data commercepkg.DirectVideoJob `json:"data"`
	}
	require.NoError(t, json.Unmarshal(cancelResponse.Body.Bytes(), &cancelledEnvelope))
	require.Equal(t, "cancelling", cancelledEnvelope.Data.Status)

	replayRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/projects/"+firstEnvelope.Data.ID+"/commerce/direct-videos/"+
			directVideoEnvelope.Data.ID+"/cancel",
		bytes.NewBufferString(`{"reason":"集成测试取消"}`),
	)
	replayRequest.Header.Set("Content-Type", "application/json")
	replayRequest.Header.Set("Authorization", "Bearer "+registration.AccessToken)
	replayRequest.Header.Set("X-Organization-Id", registration.OrganizationID)
	replayRequest.Header.Set("Idempotency-Key", "commerce-direct-video-cancel-"+suffix)
	replayResponse := httptest.NewRecorder()
	handler.ServeHTTP(replayResponse, replayRequest)
	require.Equal(t, http.StatusAccepted, replayResponse.Code, replayResponse.Body.String())

	conflictRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/projects/"+firstEnvelope.Data.ID+"/commerce/direct-videos/"+
			directVideoEnvelope.Data.ID+"/cancel",
		bytes.NewBufferString(`{"reason":"不同的取消原因"}`),
	)
	conflictRequest.Header.Set("Content-Type", "application/json")
	conflictRequest.Header.Set("Authorization", "Bearer "+registration.AccessToken)
	conflictRequest.Header.Set("X-Organization-Id", registration.OrganizationID)
	conflictRequest.Header.Set("Idempotency-Key", "commerce-direct-video-cancel-"+suffix)
	conflictResponse := httptest.NewRecorder()
	handler.ServeHTTP(conflictResponse, conflictRequest)
	assertCommerceProjectError(
		t, conflictResponse, http.StatusConflict, "IDEMPOTENCY_KEY_CONFLICT",
	)
	directWorkflowInput := workflows.CommerceDirectVideoInput{
		OrganizationID: registration.OrganizationID,
		ProjectID:      firstEnvelope.Data.ID,
		ScriptUnitID:   unitEnvelope.Data.ScriptUnit.ID,
		JobID:          directVideoEnvelope.Data.ID,
		WorkflowRunID:  *directVideoEnvelope.Data.WorkflowRunID,
		CreatedBy:      registration.User.ID,

		AttemptGeneration: 1,
	}
	directActivities := workflows.NewActivities(pool, nil, nil)
	require.NoError(t, directActivities.CancelCommerceDirectVideo(ctx, directWorkflowInput))
	require.NoError(t, directActivities.CancelCommerceDirectVideo(ctx, directWorkflowInput))
	var cancelledJobStatus, cancelledWorkflowStatus string
	var cancelledEventCount int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT
			(SELECT status FROM commerce_direct_video_jobs WHERE id = $1),
			(SELECT status FROM workflow_runs WHERE id = $2),
			(SELECT count(*) FROM project_event_log
			 WHERE project_id = $3
			   AND aggregate_type = 'commerce_direct_video_job'
			   AND aggregate_id = $1
			   AND event_type = 'commerce.direct_video.cancelled')
	`, directVideoEnvelope.Data.ID, *directVideoEnvelope.Data.WorkflowRunID, firstEnvelope.Data.ID).Scan(
		&cancelledJobStatus, &cancelledWorkflowStatus, &cancelledEventCount,
	))
	require.Equal(t, "cancelled", cancelledJobStatus)
	require.Equal(t, "cancelled", cancelledWorkflowStatus)
	require.Equal(t, 1, cancelledEventCount)
	exerciseCommerceDirectVideoProviderStub(
		t, ctx, pool, handler,
		registration.OrganizationID, firstEnvelope.Data.ID, registration.User.ID,
		registration.AccessToken, unitEnvelope.Data.ScriptUnit.ID,
		productReferenceID, providerAccountID, providerModelID, suffix,
	)
	exerciseCommerceScriptDerivationAPI(
		t, ctx, pool, handler,
		registration.OrganizationID, firstEnvelope.Data.ID,
		registration.AccessToken, unitEnvelope.Data.ScriptUnit.ID, suffix,
	)

	defaultsBody, err := json.Marshal(map[string]any{
		"expectedRevision":      firstEnvelope.Data.Revision,
		"targetDurationSeconds": 12,
		"targetPlatform":        "tiktok",
		"languageMode":          "auto",
		"targetLanguage":        nil,
	})
	require.NoError(t, err)
	defaultsRequest := httptest.NewRequest(http.MethodPatch, "/api/projects/"+firstEnvelope.Data.ID+"/commerce/script-unit-defaults", bytes.NewReader(defaultsBody))
	defaultsRequest.Header.Set("Content-Type", "application/json")
	defaultsRequest.Header.Set("Authorization", "Bearer "+registration.AccessToken)
	defaultsRequest.Header.Set("X-Organization-Id", registration.OrganizationID)
	defaultsResponse := httptest.NewRecorder()
	handler.ServeHTTP(defaultsResponse, defaultsRequest)
	require.Equal(t, http.StatusOK, defaultsResponse.Code, defaultsResponse.Body.String())
	updatedDefaults := decodeCommerceProjectEnvelope(t, defaultsResponse)
	require.Equal(t, firstEnvelope.Data.Revision+1, updatedDefaults.Data.Revision)
	require.Equal(t, &commercepkg.ScriptUnitDefaults{
		TargetDurationSeconds: 12,
		TargetPlatform:        "tiktok",
		LanguageMode:          "auto",
	}, updatedDefaults.Data.ScriptUnitDefaults)

	staleDefaultsRequest := httptest.NewRequest(http.MethodPatch, "/api/projects/"+firstEnvelope.Data.ID+"/commerce/script-unit-defaults", bytes.NewReader(defaultsBody))
	staleDefaultsRequest.Header.Set("Content-Type", "application/json")
	staleDefaultsRequest.Header.Set("Authorization", "Bearer "+registration.AccessToken)
	staleDefaultsRequest.Header.Set("X-Organization-Id", registration.OrganizationID)
	staleDefaultsResponse := httptest.NewRecorder()
	handler.ServeHTTP(staleDefaultsResponse, staleDefaultsRequest)
	assertCommerceProjectError(t, staleDefaultsResponse, http.StatusConflict, "PROJECT_REVISION_CONFLICT")

	unitDetailRequest := httptest.NewRequest(http.MethodGet, "/api/projects/"+firstEnvelope.Data.ID+"/commerce/script-units/"+unitEnvelope.Data.ScriptUnit.ID, nil)
	unitDetailRequest.Header.Set("Authorization", "Bearer "+registration.AccessToken)
	unitDetailRequest.Header.Set("X-Organization-Id", registration.OrganizationID)
	unitDetailResponse := httptest.NewRecorder()
	handler.ServeHTTP(unitDetailResponse, unitDetailRequest)
	require.Equal(t, http.StatusOK, unitDetailResponse.Code, unitDetailResponse.Body.String())
	var unitDetailEnvelope struct {
		Data commercepkg.ScriptUnit `json:"data"`
	}
	require.NoError(t, json.Unmarshal(unitDetailResponse.Body.Bytes(), &unitDetailEnvelope))
	require.Equal(t, 6, unitDetailEnvelope.Data.TargetDurationSeconds)
	require.Equal(t, "douyin", unitDetailEnvelope.Data.TargetPlatform)
	var defaultsEventCount int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(*) FROM project_event_log
		WHERE project_id = $1 AND event_type = 'commerce.project.defaults.updated'
	`, firstEnvelope.Data.ID).Scan(&defaultsEventCount))
	require.Equal(t, 1, defaultsEventCount)

	unitStatusRequest := httptest.NewRequest(http.MethodGet, "/api/projects/"+firstEnvelope.Data.ID+"/commerce/script-units/"+unitEnvelope.Data.ScriptUnit.ID+"/production-status", nil)
	unitStatusRequest.Header.Set("Authorization", "Bearer "+registration.AccessToken)
	unitStatusRequest.Header.Set("X-Organization-Id", registration.OrganizationID)
	unitStatusResponse := httptest.NewRecorder()
	handler.ServeHTTP(unitStatusResponse, unitStatusRequest)
	require.Equal(t, http.StatusOK, unitStatusResponse.Code, unitStatusResponse.Body.String())
	var unitStatusEnvelope struct {
		Data commerceUnitProductionStatus `json:"data"`
	}
	require.NoError(t, json.Unmarshal(unitStatusResponse.Body.Bytes(), &unitStatusEnvelope))
	require.Equal(t, unitEnvelope.Data.ScriptUnit.ID, unitStatusEnvelope.Data.ScriptUnitID)
	require.Equal(t, "prepare_script_unit", unitStatusEnvelope.Data.NextAction)

	unitListRequest := httptest.NewRequest(http.MethodGet, "/api/projects/"+firstEnvelope.Data.ID+"/commerce/script-units?include=productionSummary", nil)
	unitListRequest.Header.Set("Authorization", "Bearer "+registration.AccessToken)
	unitListRequest.Header.Set("X-Organization-Id", registration.OrganizationID)
	unitListResponse := httptest.NewRecorder()
	handler.ServeHTTP(unitListResponse, unitListRequest)
	require.Equal(t, http.StatusOK, unitListResponse.Code, unitListResponse.Body.String())
	var unitListEnvelope struct {
		Data commercepkg.ScriptUnitList `json:"data"`
	}
	require.NoError(t, json.Unmarshal(unitListResponse.Body.Bytes(), &unitListEnvelope))
	require.Len(t, unitListEnvelope.Data.Items, 1)
	require.NotNil(t, unitListEnvelope.Data.Items[0].ProductionSummary)
	require.Equal(t, "draft", unitListEnvelope.Data.Items[0].ProductionSummary.CurrentStage)
}

func exerciseCommerceDirectVideoProviderStub(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	handler http.Handler,
	organizationID string,
	projectID string,
	userID string,
	accessToken string,
	scriptUnitID string,
	productReferenceID string,
	providerAccountID string,
	providerModelID string,
	suffix string,
) {
	t.Helper()
	const expectedScript = "展示产品卖点并引导下单。"
	requestBody, err := json.Marshal(map[string]any{
		"durationSeconds": 6,
		"resolution":      "720p",
		"aspectRatio":     "9:16",
		"generateAudio":   true,
		"references": []map[string]any{{
			"sourceType": "product",
			"sourceId":   productReferenceID,
		}},
	})
	require.NoError(t, err)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/projects/"+projectID+"/commerce/script-units/"+scriptUnitID+"/direct-videos",
		bytes.NewReader(requestBody),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("X-Organization-Id", organizationID)
	request.Header.Set("Idempotency-Key", "commerce-direct-video-provider-stub-"+suffix)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusAccepted, response.Code, response.Body.String())
	var envelope struct {
		Data commercepkg.DirectVideoJob `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	require.NotNil(t, envelope.Data.WorkflowRunID)
	require.Equal(t, expectedScript, envelope.Data.ScriptSnapshot)

	var providerRequestID, providerCallID, providerAsyncTaskID string
	var outputArtifactID, outputMediaFileID string
	outputStorageKey := "commerce/direct-video-output-" + suffix + ".mp4"
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/internal/provider/video/create-task":
			var gatewayRequest provider.GatewayVideoCreateTaskRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&gatewayRequest))
			require.Equal(t, organizationID, gatewayRequest.OrganizationID)
			require.Equal(t, projectID, gatewayRequest.ProjectID)
			require.Equal(t, envelope.Data.ID, gatewayRequest.CommerceDirectVideoJobID)
			require.Equal(t, providerModelID, gatewayRequest.ProviderModelID)
			require.Len(t, gatewayRequest.References, 1)
			require.Equal(t, productReferenceID, gatewayRequest.References[0].SourceID)
			var input struct {
				Prompt           string `json:"prompt"`
				Duration         int    `json:"duration"`
				AspectRatio      string `json:"aspectRatio"`
				GenerateAudio    bool   `json:"generateAudio"`
				DirectVideoJobID string `json:"commerceDirectVideoJobId"`
			}
			require.NoError(t, json.Unmarshal(gatewayRequest.Input, &input))
			require.Equal(t, expectedScript, input.Prompt)
			require.Equal(t, 6, input.Duration)
			require.Equal(t, "9:16", input.AspectRatio)
			require.True(t, input.GenerateAudio)
			require.Equal(t, envelope.Data.ID, input.DirectVideoJobID)
			require.NoError(t, pool.QueryRow(ctx, `
				INSERT INTO provider_requests(
					organization_id, project_id, workflow_run_id, node_run_id,
					task_type, idempotency_key, request_hash, status, started_at,
					production_generation_id, video_production_binding_id,
					video_production_binding_revision
				)
				VALUES (
					$1, $2, $3, $4, 'video.create_task', $5, $6, 'running', now(),
					$7, $8, $9
				)
				RETURNING id::text
			`, organizationID, projectID, gatewayRequest.WorkflowRunID, gatewayRequest.NodeRunID,
				gatewayRequest.IdempotencyKey, strings.Repeat("a", 64),
				gatewayRequest.ProductionGenerationID, gatewayRequest.VideoProductionBindingID,
				gatewayRequest.VideoProductionBindingRevision).Scan(&providerRequestID))
			require.NoError(t, pool.QueryRow(ctx, `
				INSERT INTO provider_call_logs(
					organization_id, project_id, workflow_run_id, node_run_id,
					provider_request_id, provider_account_id, provider_model_id,
					task_type, execution_mode, status, request_hash, request_snapshot,
					started_at, production_generation_id
				)
				VALUES (
					$1, $2, $3, $4, $5, $6, $7,
					'video.create_task', 'async_create', 'running', $8, $9, now(), $10
				)
				RETURNING id::text
			`, organizationID, projectID, gatewayRequest.WorkflowRunID, gatewayRequest.NodeRunID,
				providerRequestID, providerAccountID, providerModelID, strings.Repeat("a", 64),
				gatewayRequest.Input, gatewayRequest.ProductionGenerationID).Scan(&providerCallID))
			require.NoError(t, pool.QueryRow(ctx, `
				INSERT INTO provider_async_tasks(
					provider_call_id, provider_request_id, organization_id, project_id,
					workflow_run_id, node_run_id, node_execution_token,
					node_attempt_generation, provider_account_id, provider_model_id,
					external_task_id, status, task_type, execution_mode, input,
					raw_status, started_at, production_generation_id,
					video_production_binding_id, video_production_binding_revision
				)
				VALUES (
					$1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
					$11, 'running', 'video.generate', 'async_polling', $12,
					'{"status":"running"}', now(), $13, $14, $15
				)
				RETURNING id::text
			`, providerCallID, providerRequestID, organizationID, projectID,
				gatewayRequest.WorkflowRunID, gatewayRequest.NodeRunID,
				gatewayRequest.NodeExecutionToken, gatewayRequest.NodeAttemptGeneration,
				providerAccountID, providerModelID, "stub-video-"+suffix,
				gatewayRequest.Input, gatewayRequest.ProductionGenerationID,
				gatewayRequest.VideoProductionBindingID,
				gatewayRequest.VideoProductionBindingRevision).Scan(&providerAsyncTaskID))
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"data": provider.GatewayVideoCreateTaskResponse{
					ProviderRequestID: providerRequestID, AttemptGeneration: 1,
					ProviderCallID: providerCallID, ProviderAsyncTaskID: providerAsyncTaskID,
					ExternalTaskID: "stub-video-" + suffix, ModelID: providerModelID,
					Status: "running",
				},
			}))
		case "/internal/provider/video/poll-task":
			var gatewayRequest provider.GatewayVideoPollTaskRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&gatewayRequest))
			require.Equal(t, providerAsyncTaskID, gatewayRequest.ProviderAsyncTaskID)
			require.NoError(t, pool.QueryRow(ctx, `
				INSERT INTO artifacts(
					organization_id, project_id, production_generation_id,
					workflow_run_id, type, storage_key, mime_type, content_hash,
					metadata, created_by
				)
				VALUES (
					$1, $2, $3, $4, 'commerce_direct_video', $5,
					'video/mp4', $6, '{"providerStub":true}', $7
				)
				RETURNING id::text
			`, organizationID, projectID, gatewayRequest.ProductionGenerationID,
				gatewayRequest.WorkflowRunID, outputStorageKey, strings.Repeat("b", 64),
				userID).Scan(&outputArtifactID))
			require.NoError(t, pool.QueryRow(ctx, `
				INSERT INTO media_files(
					organization_id, project_id, production_generation_id,
					artifact_id, storage_key, mime_type, byte_size,
					duration_seconds, width, height, video_stream_count,
					audio_stream_count, checksum, metadata, created_by
				)
				VALUES (
					$1, $2, $3, $4, $5, 'video/mp4', 1024,
					6, 720, 1280, 1, 1, $6, '{"providerStub":true}', $7
				)
				RETURNING id::text
			`, organizationID, projectID, gatewayRequest.ProductionGenerationID,
				outputArtifactID, outputStorageKey, strings.Repeat("b", 64),
				userID).Scan(&outputMediaFileID))
			_, err = pool.Exec(ctx, `
				UPDATE provider_requests
				SET status = 'succeeded', artifact_ids = jsonb_build_array($2::text),
				    media_file_ids = jsonb_build_array($3::text), completed_at = now()
				WHERE id = $1
			`, providerRequestID, outputArtifactID, outputMediaFileID)
			require.NoError(t, err)
			_, err = pool.Exec(ctx, `
				UPDATE provider_call_logs
				SET status = 'succeeded', artifact_ids = jsonb_build_array($2::text),
				    media_file_ids = jsonb_build_array($3::text), completed_at = now()
				WHERE id = $1
			`, providerCallID, outputArtifactID, outputMediaFileID)
			require.NoError(t, err)
			_, err = pool.Exec(ctx, `
				UPDATE provider_async_tasks
				SET status = 'succeeded', completed_at = now(), finalized_at = now(),
				    normalized_output = jsonb_build_object(
				        'artifactId', $2::text, 'mediaFileId', $3::text,
				        'storageKey', $4::text
				    )
				WHERE id = $1
			`, providerAsyncTaskID, outputArtifactID, outputMediaFileID, outputStorageKey)
			require.NoError(t, err)
			duration := float64(6)
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"data": provider.GatewayVideoPollTaskResponse{
					ProviderRequestID: providerRequestID, AttemptGeneration: 1,
					ProviderCallID: providerCallID, ProviderAsyncTaskID: providerAsyncTaskID,
					ExternalTaskID: "stub-video-" + suffix, ModelID: providerModelID,
					Status: "succeeded",
					Output: provider.GatewayVideoOutput{
						ArtifactID: outputArtifactID, MediaFileID: outputMediaFileID,
						StorageKey: outputStorageKey, MimeType: "video/mp4",
						DurationSeconds: &duration,
					},
				},
			}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer gateway.Close()

	workflowInput := workflows.CommerceDirectVideoInput{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		ScriptUnitID:   scriptUnitID,
		JobID:          envelope.Data.ID,
		WorkflowRunID:  *envelope.Data.WorkflowRunID,
		CreatedBy:      userID,

		AttemptGeneration: 1,
	}
	activities := workflows.NewActivities(
		pool, nil, &provider.GatewayClient{BaseURL: gateway.URL, Client: gateway.Client()},
	)
	var suite testsuite.WorkflowTestSuite
	activityEnvironment := suite.NewTestActivityEnvironment()
	activityEnvironment.RegisterActivity(activities.CreateCommerceDirectVideoTask)
	activityEnvironment.RegisterActivity(activities.PollCommerceDirectVideoTask)
	activityEnvironment.RegisterActivity(activities.CompleteCommerceDirectVideo)
	createdValue, err := activityEnvironment.ExecuteActivity(
		workflows.CreateCommerceDirectVideoActivity, workflowInput,
	)
	require.NoError(t, err)
	var created workflows.CommerceDirectVideoTaskState
	require.NoError(t, createdValue.Get(&created))
	require.Equal(t, "running", created.Status)
	require.NotEmpty(t, created.ProviderAsyncTaskID)
	polledValue, err := activityEnvironment.ExecuteActivity(
		workflows.PollCommerceDirectVideoActivity, workflowInput,
	)
	require.NoError(t, err)
	var polled workflows.CommerceDirectVideoTaskState
	require.NoError(t, polledValue.Get(&polled))
	require.Equal(t, "succeeded", polled.Status)
	require.Equal(t, outputArtifactID, polled.Output.ArtifactID)
	_, err = activityEnvironment.ExecuteActivity(
		workflows.CompleteCommerceDirectVideoActivity,
		workflowInput, workflows.CommerceDirectVideoOutput{
			JobID:             envelope.Data.ID,
			Status:            "succeeded",
			OutputArtifactID:  outputArtifactID,
			OutputMediaFileID: outputMediaFileID,
			OutputStorageKey:  outputStorageKey,
		},
	)
	require.NoError(t, err)

	var jobStatus, workflowStatus, scriptSnapshot string
	var persistedRequestID, persistedCallID, persistedTaskID string
	var succeededEventCount int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT job.status, run.status, job.script_snapshot,
		       job.provider_request_id::text, job.provider_call_id::text,
		       job.provider_async_task_id::text,
		       (SELECT count(*) FROM project_event_log
		        WHERE project_id = job.project_id
		          AND aggregate_type = 'commerce_direct_video_job'
		          AND aggregate_id = job.id
		          AND event_type = 'commerce.direct_video.succeeded')
		FROM commerce_direct_video_jobs job
		JOIN workflow_runs run ON run.id = job.workflow_run_id
		WHERE job.id = $1
	`, envelope.Data.ID).Scan(
		&jobStatus, &workflowStatus, &scriptSnapshot,
		&persistedRequestID, &persistedCallID, &persistedTaskID,
		&succeededEventCount,
	))
	require.Equal(t, "succeeded", jobStatus)
	require.Equal(t, "succeeded", workflowStatus)
	require.Equal(t, expectedScript, scriptSnapshot)
	require.Equal(t, providerRequestID, persistedRequestID)
	require.Equal(t, providerCallID, persistedCallID)
	require.Equal(t, providerAsyncTaskID, persistedTaskID)
	require.Equal(t, 1, succeededEventCount)
}

func exerciseCommerceScriptDerivationAPI(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	handler http.Handler,
	organizationID string,
	projectID string,
	accessToken string,
	scriptUnitID string,
	suffix string,
) {
	t.Helper()
	payload := map[string]any{
		"dimension":   "scene",
		"instruction": "仅替换使用场景，保持商品事实、语言和行动号召不变",
		"preserve":    []string{"product_facts", "language", "cta"},
		"variations": []map[string]any{
			{"key": "night_market", "label": "夜市场景", "brief": "真实夜市摊位体验"},
			{"key": "home_living_room", "label": "家庭客厅", "brief": "居家日常体验"},
		},
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	endpoint := "/api/projects/" + projectID + "/commerce/script-units/" + scriptUnitID + "/derivations"
	missingKeyRequest := httptest.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	missingKeyRequest.Header.Set("Content-Type", "application/json")
	missingKeyRequest.Header.Set("Authorization", "Bearer "+accessToken)
	missingKeyRequest.Header.Set("X-Organization-Id", organizationID)
	missingKeyResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingKeyResponse, missingKeyRequest)
	assertCommerceProjectError(
		t, missingKeyResponse, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REQUIRED",
	)

	idempotencyKey := "commerce-script-derivation-" + suffix
	createRequest := httptest.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	createRequest.Header.Set("Content-Type", "application/json")
	createRequest.Header.Set("Authorization", "Bearer "+accessToken)
	createRequest.Header.Set("X-Organization-Id", organizationID)
	createRequest.Header.Set("Idempotency-Key", idempotencyKey)
	createResponse := httptest.NewRecorder()
	handler.ServeHTTP(createResponse, createRequest)
	require.Equal(t, http.StatusAccepted, createResponse.Code, createResponse.Body.String())
	var created struct {
		Data commercepkg.ScriptDerivationBatch `json:"data"`
	}
	require.NoError(t, json.Unmarshal(createResponse.Body.Bytes(), &created))
	require.Equal(t, "queued", created.Data.Status)
	require.Equal(t, 2, created.Data.RequestedCount)
	var createdItemCount int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(*) FROM commerce_script_derivation_items WHERE batch_id = $1
	`, created.Data.ID).Scan(&createdItemCount))
	require.Equal(t, 2, createdItemCount)
	require.NotNil(t, created.Data.WorkflowRunID)
	require.Equal(t, "展示产品卖点并引导下单。", created.Data.SourceContentSnapshot)
	require.NotEmpty(t, created.Data.RoutingSnapshotHash)
	require.NotEmpty(t, created.Data.PromptContract.Generator.PromptVersionID)

	replayRequest := httptest.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	replayRequest.Header.Set("Content-Type", "application/json")
	replayRequest.Header.Set("Authorization", "Bearer "+accessToken)
	replayRequest.Header.Set("X-Organization-Id", organizationID)
	replayRequest.Header.Set("Idempotency-Key", idempotencyKey)
	replayResponse := httptest.NewRecorder()
	handler.ServeHTTP(replayResponse, replayRequest)
	require.Equal(t, http.StatusAccepted, replayResponse.Code, replayResponse.Body.String())
	var replayed struct {
		Data commercepkg.ScriptDerivationBatch `json:"data"`
		Meta struct {
			IdempotentReplay bool `json:"idempotentReplay"`
		} `json:"meta"`
	}
	require.NoError(t, json.Unmarshal(replayResponse.Body.Bytes(), &replayed))
	require.Equal(t, created.Data.ID, replayed.Data.ID)
	require.True(t, replayed.Meta.IdempotentReplay)

	conflictPayload := map[string]any{}
	for key, value := range payload {
		conflictPayload[key] = value
	}
	conflictPayload["instruction"] = "使用同一请求标识但改变裂变要求"
	conflictBody, err := json.Marshal(conflictPayload)
	require.NoError(t, err)
	conflictRequest := httptest.NewRequest(http.MethodPost, endpoint, bytes.NewReader(conflictBody))
	conflictRequest.Header.Set("Content-Type", "application/json")
	conflictRequest.Header.Set("Authorization", "Bearer "+accessToken)
	conflictRequest.Header.Set("X-Organization-Id", organizationID)
	conflictRequest.Header.Set("Idempotency-Key", idempotencyKey)
	conflictResponse := httptest.NewRecorder()
	handler.ServeHTTP(conflictResponse, conflictRequest)
	assertCommerceProjectError(
		t, conflictResponse, http.StatusConflict, "IDEMPOTENCY_KEY_CONFLICT",
	)

	getRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/projects/"+projectID+"/commerce/script-derivations/"+created.Data.ID+"?include=lineage",
		nil,
	)
	getRequest.Header.Set("Authorization", "Bearer "+accessToken)
	getRequest.Header.Set("X-Organization-Id", organizationID)
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, getRequest)
	require.Equal(t, http.StatusOK, getResponse.Code, getResponse.Body.String())

	listRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/projects/"+projectID+"/commerce/script-derivations?filter[sourceScriptUnitId]="+scriptUnitID,
		nil,
	)
	listRequest.Header.Set("Authorization", "Bearer "+accessToken)
	listRequest.Header.Set("X-Organization-Id", organizationID)
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	require.Equal(t, http.StatusOK, listResponse.Code, listResponse.Body.String())
	var listed struct {
		Data commercepkg.ScriptDerivationBatchList `json:"data"`
	}
	require.NoError(t, json.Unmarshal(listResponse.Body.Bytes(), &listed))
	require.Len(t, listed.Data.Items, 1)
	require.Equal(t, created.Data.ID, listed.Data.Items[0].ID)

	cancelEndpoint := "/api/projects/" + projectID + "/commerce/script-derivations/" + created.Data.ID + "/cancel"
	cancelBody := []byte(`{"reason":"集成测试取消裂变"}`)
	cancelRequest := httptest.NewRequest(http.MethodPost, cancelEndpoint, bytes.NewReader(cancelBody))
	cancelRequest.Header.Set("Content-Type", "application/json")
	cancelRequest.Header.Set("Authorization", "Bearer "+accessToken)
	cancelRequest.Header.Set("X-Organization-Id", organizationID)
	cancelRequest.Header.Set("Idempotency-Key", "commerce-script-derivation-cancel-"+suffix)
	cancelResponse := httptest.NewRecorder()
	handler.ServeHTTP(cancelResponse, cancelRequest)
	require.Equal(t, http.StatusAccepted, cancelResponse.Code, cancelResponse.Body.String())
	var cancelling struct {
		Data commercepkg.ScriptDerivationBatch `json:"data"`
	}
	require.NoError(t, json.Unmarshal(cancelResponse.Body.Bytes(), &cancelling))
	require.Equal(t, "cancelling", cancelling.Data.Status)

	cancelReplayRequest := httptest.NewRequest(
		http.MethodPost, cancelEndpoint, bytes.NewReader(cancelBody),
	)
	cancelReplayRequest.Header.Set("Content-Type", "application/json")
	cancelReplayRequest.Header.Set("Authorization", "Bearer "+accessToken)
	cancelReplayRequest.Header.Set("X-Organization-Id", organizationID)
	cancelReplayRequest.Header.Set("Idempotency-Key", "commerce-script-derivation-cancel-"+suffix)
	cancelReplayResponse := httptest.NewRecorder()
	handler.ServeHTTP(cancelReplayResponse, cancelReplayRequest)
	require.Equal(t, http.StatusAccepted, cancelReplayResponse.Code, cancelReplayResponse.Body.String())

	commerceActivities := workflows.NewCommerceActivities(
		workflows.NewActivities(pool, nil, nil), nil,
	)
	require.NoError(t, commerceActivities.CancelCommerceScriptDerivationBatch(
		ctx, workflows.CommerceScriptDerivationBatchInput{
			OrganizationID: organizationID, ProjectID: projectID,
			BatchID: created.Data.ID, WorkflowRunID: *created.Data.WorkflowRunID,
			MaxConcurrency: 5,
		},
	))
	var batchStatus, workflowStatus string
	var cancelledItems, cancelledEvents int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT batch.status, run.status,
		       (SELECT count(*) FROM commerce_script_derivation_items
		        WHERE batch_id = batch.id AND status = 'cancelled'),
		       (SELECT count(*) FROM project_event_log
		        WHERE project_id = batch.project_id
		          AND aggregate_type = 'commerce_script_derivation_batch'
		          AND aggregate_id = batch.id
		          AND event_type = 'commerce.script_derivation.batch.cancelled')
		FROM commerce_script_derivation_batches batch
		JOIN workflow_runs run ON run.id = batch.workflow_run_id
		WHERE batch.id = $1
	`, created.Data.ID).Scan(
		&batchStatus, &workflowStatus, &cancelledItems, &cancelledEvents,
	))
	require.Equal(t, "cancelled", batchStatus)
	require.Equal(t, "cancelled", workflowStatus)
	require.Equal(t, 2, cancelledItems)
	require.Equal(t, 1, cancelledEvents)
}

func seedCommerceWorkflowTemplate(t *testing.T, ctx context.Context, pool *pgxpool.Pool, organizationID, userID string) {
	t.Helper()
	var profileVersionID string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT version.id::text
		FROM video_production_profile_versions version
		JOIN video_production_profiles profile ON profile.id = version.profile_id
		WHERE profile.profile_key = $1
		  AND version.lifecycle_state = 'published'
		  AND version.implementation_state = 'available'
		ORDER BY version.version DESC
		LIMIT 1
	`, videoproduction.ProfileSingleFrameI2V).Scan(&profileVersionID))

	var templateID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO commerce_workflow_templates(organization_id, template_key, name, status, created_by)
		VALUES ($1, $2, 'Commerce Project API Test', 'active', $3)
		RETURNING id::text
	`, organizationID, commercepkg.DefaultWorkflowTemplateKey, userID).Scan(&templateID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO commerce_workflow_template_versions(
			template_id, version, configuration_snapshot, prompt_bindings,
			agent_model_contracts, language_contract, image_capability_contract,
			video_capability_contract, video_production_profile_version_id,
			content_hash, status, created_by, published_at
		)
		VALUES (
			$1, 1,
			'{"durations":[6,10,12,16],"aspectRatios":["9:16","16:9","1:1"],"imageQualities":["standard","hd"],"languageModes":["auto","explicit"],"audioStrategies":["native_av","external_audio"],"audioRequirements":["preferred","required","disabled"]}',
			'{}',
			'{"languageResolver":{"profileKey":"script_agent_default","label":"语言判断","taskType":"text.generate","modality":"text","usesInputLanguage":true},"storyboardPlanner":{"profileKey":"script_agent_default","label":"分镜规划","taskType":"text.generate","modality":"text","usesOutputLanguage":true}}',
			'{"locales":[{"locale":"zh-CN","label":"简体中文"},{"locale":"en-US","label":"English"}]}',
			'{"profileKey":"image_generation_default","label":"商品参考图","taskType":"image.generate","modality":"image","usesPromptLanguage":true}',
			'{"profileKey":"video_generation_default","label":"镜头视频","taskType":"video.generate","modality":"video","usesPromptLanguage":true,"usesNativeAudio":true}',
			$2, $3, 'published', $4, now()
		)
		RETURNING id::text
	`, templateID, profileVersionID, strings.Repeat("c", 64), userID).Scan(new(string)))
}

func seedCommerceDirectVideoModel(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	organizationID string,
	userID string,
) (string, string) {
	t.Helper()
	var connectorID, accountID, modelID, profileID string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT id::text
		FROM provider_connectors
		ORDER BY is_official DESC, created_at, id
		LIMIT 1
	`).Scan(&connectorID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO provider_accounts(
			organization_id, connector_id, name, base_url, auth_type, status, created_by
		)
		VALUES ($1, $2, 'Commerce Direct Video', 'https://example.invalid/v1', 'none', 'active', $3)
		RETURNING id::text
	`, organizationID, connectorID, userID).Scan(&accountID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO provider_models(provider_account_id, model_key, display_name, modality, status)
		VALUES ($1, 'commerce-direct-video', 'Commerce Direct Video', 'video', 'active')
		RETURNING id::text
	`, accountID).Scan(&modelID))
	_, err := pool.Exec(ctx, `
		INSERT INTO provider_model_capabilities(
			provider_model_id, task_types, provider_options_schema
		)
		VALUES (
			$1,
			'["video.generate","video.create_task","video.poll_task","video.cancel_task"]',
			'{"xCapabilities":{"videoGenerationVariants":[{
				"variantKey":"commerce-direct-i2v",
				"when":{"taskTypes":["video.generate"],"referenceModes":["first_frame"],"nativeAudioRequested":true},
				"duration":{"mode":"discrete","values":[6,10,12,16]},
				"resolutions":["720p"],
				"aspectRatios":["9:16","16:9","1:1"],
				"frameRate":{"mode":"unknown"},
				"nativeAudio":{"support":"true","supportsDialogue":true,"supportsVoiceover":true},
				"continuation":{"supportsFirstFrame":true},
				"requestModes":["async_create"],
				"source":"official",
				"verificationStatus":"official"
			}]}}'
		)
	`, modelID)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO model_profiles(
			organization_id, profile_key, name, purpose, routing_strategy
		)
		VALUES (
			$1, 'video_generation_default', 'Commerce Video', 'commerce direct video',
			'priority_with_fallback'
		)
		RETURNING id::text
	`, organizationID).Scan(&profileID))
	_, err = pool.Exec(ctx, `
		INSERT INTO model_profile_bindings(
			model_profile_id, provider_model_id, priority, weight, enabled
		)
		VALUES ($1, $2, 100, 100, true)
	`, profileID, modelID)
	require.NoError(t, err)
	var textModelID, textProfileID string
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO provider_models(
			provider_account_id, model_key, display_name, modality, status
		)
		VALUES ($1, 'commerce-script-agent', 'Commerce Script Agent', 'text', 'active')
		RETURNING id::text
	`, accountID).Scan(&textModelID))
	_, err = pool.Exec(ctx, `
		INSERT INTO provider_model_capabilities(
			provider_model_id, task_types, input_limits, output_limits
		)
		VALUES (
			$1, '["text.generate"]',
			'{"maxInputTokens":32000}', '{"maxOutputTokens":8000}'
		)
	`, textModelID)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO model_profiles(
			organization_id, profile_key, name, purpose, routing_strategy
		)
		VALUES (
			$1, 'script_agent_default', 'Commerce Script Agent',
			'commerce script derivation', 'priority_with_fallback'
		)
		RETURNING id::text
	`, organizationID).Scan(&textProfileID))
	_, err = pool.Exec(ctx, `
		INSERT INTO model_profile_bindings(
			model_profile_id, provider_model_id, priority, weight, enabled
		)
		VALUES ($1, $2, 100, 100, true)
	`, textProfileID, textModelID)
	require.NoError(t, err)
	return accountID, modelID
}

func doCommerceProjectCreateRequest(
	t *testing.T,
	handler http.Handler,
	token string,
	organizationID string,
	idempotencyKey string,
	body map[string]any,
) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/api/projects", bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-Organization-Id", organizationID)
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

type commerceProjectEnvelope struct {
	Data Project        `json:"data"`
	Meta map[string]any `json:"meta,omitempty"`
}

func decodeCommerceProjectEnvelope(t *testing.T, recorder *httptest.ResponseRecorder) commerceProjectEnvelope {
	t.Helper()
	var envelope commerceProjectEnvelope
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	return envelope
}

func assertCommerceProjectError(t *testing.T, recorder *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	require.Equal(t, status, recorder.Code, recorder.Body.String())
	var envelope httpx.Envelope
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.NotNil(t, envelope.Error)
	require.Equal(t, code, envelope.Error.Code)
}

func cloneCommerceCreateBody(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func dereferenceString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
