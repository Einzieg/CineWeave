package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/db"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/google/uuid"
)

func TestPrompts(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run prompt API integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for prompt API integration tests")
	}

	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer pool.Close()

	authService := auth.NewService(pool, "prompt-test-secret", time.Hour, 24*time.Hour)
	vault, err := provider.NewVault("")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	providerService := provider.NewService(pool, vault)
	server := New(pool, authService, providerService, nil, nil).Handler()

	suffix := uuid.NewString()
	owner, err := authService.Register(ctx, auth.RegisterRequest{
		Email:            "prompt-owner-" + suffix + "@example.test",
		Password:         "Password123!",
		DisplayName:      "Prompt Owner",
		OrganizationName: "Prompt Org " + suffix,
	}, httptest.NewRequest(http.MethodPost, "/api/auth/register", nil))
	if err != nil {
		t.Fatalf("register owner: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, owner.OrganizationID)
	})
	member, err := authService.Register(ctx, auth.RegisterRequest{
		Email:            "prompt-member-" + suffix + "@example.test",
		Password:         "Password123!",
		DisplayName:      "Prompt Member",
		OrganizationName: "Prompt Member Org " + suffix,
	}, httptest.NewRequest(http.MethodPost, "/api/auth/register", nil))
	if err != nil {
		t.Fatalf("register member: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, member.OrganizationID)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO organization_members(organization_id, user_id, status) VALUES ($1, $2, 'active')`, owner.OrganizationID, member.User.ID); err != nil {
		t.Fatalf("insert member org membership: %v", err)
	}

	var list struct {
		Items []PromptTemplate `json:"items"`
	}
	doAPISuccess(t, server, http.MethodGet, "/api/prompt-templates", owner.AccessToken, owner.OrganizationID, nil, &list)
	if !promptTemplateListHasKey(list.Items, "storyboard_planner") {
		t.Fatalf("system storyboard prompt not listed: %+v", list.Items)
	}

	templateKey := "prompt_api_test_" + strings.ReplaceAll(suffix, "-", "")
	var template PromptTemplate
	doAPISuccess(t, server, http.MethodPost, "/api/prompt-templates", owner.AccessToken, owner.OrganizationID, map[string]any{
		"organizationId": owner.OrganizationID,
		"templateKey":    templateKey,
		"name":           "Prompt API Test",
		"purpose":        "test",
		"modality":       "text",
		"taskType":       "text.generate",
	}, &template)

	var version PromptVersion
	doAPISuccess(t, server, http.MethodPost, "/api/prompt-templates/"+template.ID+"/versions", owner.AccessToken, owner.OrganizationID, map[string]any{
		"title":   "Draft v1",
		"content": "Hello {{ input.prompt }}",
	}, &version)
	if version.ID == "" || version.Status != "draft" {
		t.Fatalf("created version = %+v", version)
	}

	assertAPIErrorCode(t, server, http.MethodPost, "/api/prompt-versions/"+version.ID+"/activate", member.AccessToken, owner.OrganizationID, nil, http.StatusForbidden, "ACCESS_DENIED")

	var active PromptVersion
	doAPISuccess(t, server, http.MethodPost, "/api/prompt-versions/"+version.ID+"/activate", owner.AccessToken, owner.OrganizationID, nil, &active)
	if active.Status != "active" {
		t.Fatalf("active version = %+v", active)
	}

	var rendered struct {
		TemplateKey     string `json:"templateKey"`
		PromptVersionID string `json:"promptVersionId"`
		RenderedHash    string `json:"renderedHash"`
		Text            string `json:"text"`
	}
	doAPISuccess(t, server, http.MethodPost, "/api/prompts/render-test", owner.AccessToken, owner.OrganizationID, map[string]any{
		"organizationId": owner.OrganizationID,
		"templateKey":    templateKey,
		"variables": map[string]any{
			"input": map[string]any{"prompt": "station"},
		},
	}, &rendered)
	if rendered.TemplateKey != templateKey || rendered.PromptVersionID != version.ID || !strings.HasPrefix(rendered.RenderedHash, "sha256:") || rendered.Text != "Hello station" {
		t.Fatalf("rendered prompt = %+v", rendered)
	}
}

func promptTemplateListHasKey(items []PromptTemplate, key string) bool {
	for _, item := range items {
		if item.TemplateKey == key {
			return true
		}
	}
	return false
}
