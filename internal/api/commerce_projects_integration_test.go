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
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
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
	handler := New(pool, authService, nil, nil, nil).Handler()
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
	require.Equal(t, []int{15, 30, 60}, optionsEnvelope.Data.Durations)
	require.Len(t, optionsEnvelope.Data.Languages, 2)
	require.Len(t, optionsEnvelope.Data.ModelRequirements, 4)
	require.False(t, optionsEnvelope.Data.Available, "provider readiness must be applied without an upstream call")
	require.Contains(t, optionsEnvelope.Data.Blockers, "供应商运行时尚未配置")

	key := "commerce-create-" + suffix
	body := map[string]any{
		"workspaceId":                  registration.WorkspaceID,
		"name":                         "多语言商品视频 " + suffix,
		"projectKind":                  "commerce_video",
		"videoRatio":                   "9:16",
		"defaultTargetDurationSeconds": 30,
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
		TargetDurationSeconds: 30,
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
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO commerce_products(organization_id, project_id, status, created_by)
		VALUES ($1, $2, 'draft', $3)
		RETURNING id::text
	`, registration.OrganizationID, firstEnvelope.Data.ID, registration.User.ID).Scan(new(string)))

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
		"targetDurationSeconds":       30,
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

	defaultsBody, err := json.Marshal(map[string]any{
		"expectedRevision":      firstEnvelope.Data.Revision,
		"targetDurationSeconds": 60,
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
		TargetDurationSeconds: 60,
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
	require.Equal(t, 30, unitDetailEnvelope.Data.TargetDurationSeconds)
	require.Equal(t, "douyin", unitDetailEnvelope.Data.TargetPlatform)
	var defaultsEventCount int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(*) FROM events
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
			'{"durations":[15,30,60],"aspectRatios":["9:16","16:9","1:1"],"imageQualities":["standard","hd"],"languageModes":["auto","explicit"],"audioStrategies":["native_av","external_audio"],"audioRequirements":["preferred","required","disabled"]}',
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
