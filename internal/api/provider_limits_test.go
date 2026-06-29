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

func TestProviderLimitPolicyAPI(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run provider limit API integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for provider limit API integration tests")
	}

	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer pool.Close()

	authService := auth.NewService(pool, "provider-limit-test-secret", time.Hour, 24*time.Hour)
	vault, err := provider.NewVault("")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	providerService := provider.NewService(pool, vault)
	server := New(pool, authService, providerService, nil, nil).Handler()
	ensureRBACProviderConnector(t, ctx, pool)

	suffix := uuid.NewString()
	owner, err := authService.Register(ctx, auth.RegisterRequest{
		Email:            "provider-limit-owner-" + suffix + "@example.test",
		Password:         "Password123!",
		DisplayName:      "Provider Limit Owner",
		OrganizationName: "Provider Limit Org " + suffix,
	}, httptest.NewRequest(http.MethodPost, "/api/auth/register", nil))
	if err != nil {
		t.Fatalf("register owner: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, owner.OrganizationID)
	})
	member, err := authService.Register(ctx, auth.RegisterRequest{
		Email:            "provider-limit-member-" + suffix + "@example.test",
		Password:         "Password123!",
		DisplayName:      "Provider Limit Member",
		OrganizationName: "Provider Limit Member Org " + suffix,
	}, httptest.NewRequest(http.MethodPost, "/api/auth/register", nil))
	if err != nil {
		t.Fatalf("register member: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, member.OrganizationID)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO organization_members(organization_id, user_id, status) VALUES ($1, $2, 'active')`, owner.OrganizationID, member.User.ID); err != nil {
		t.Fatalf("insert owner org membership: %v", err)
	}

	var account provider.Account
	doAPISuccess(t, server, http.MethodPost, "/api/providers/accounts", owner.AccessToken, owner.OrganizationID, providerAccountBody(owner.OrganizationID), &account)
	var model provider.Model
	doAPISuccess(t, server, http.MethodPost, "/api/providers/accounts/"+account.ID+"/models", owner.AccessToken, owner.OrganizationID, map[string]any{
		"modelKey":    "limit-api-model",
		"displayName": "Limit API Model",
		"modality":    "text",
		"status":      "active",
	}, &model)

	createBody := map[string]any{
		"organizationId":       owner.OrganizationID,
		"providerAccountId":    account.ID,
		"providerModelId":      model.ID,
		"taskType":             "video.create_task",
		"maxConcurrency":       2,
		"requestsPerMinute":    10,
		"dailyBudget":          "20.00000000",
		"failureThreshold":     3,
		"failureWindowSeconds": 300,
		"enabled":              true,
	}
	assertAPIErrorCode(t, server, http.MethodPost, "/api/provider-limit-policies", member.AccessToken, owner.OrganizationID, createBody, http.StatusForbidden, "ACCESS_DENIED")

	var created provider.ProviderLimitPolicy
	doAPISuccess(t, server, http.MethodPost, "/api/provider-limit-policies", owner.AccessToken, owner.OrganizationID, createBody, &created)
	if created.TaskType != "video.create_task" || created.MaxConcurrency == nil || *created.MaxConcurrency != 2 {
		t.Fatalf("created policy = %+v", created)
	}

	var list struct {
		Items []provider.ProviderLimitPolicy `json:"items"`
	}
	doAPISuccess(t, server, http.MethodGet, "/api/provider-limit-policies", owner.AccessToken, owner.OrganizationID, nil, &list)
	if len(list.Items) == 0 {
		t.Fatal("expected at least one provider limit policy")
	}

	var updated provider.ProviderLimitPolicy
	doAPISuccess(t, server, http.MethodPatch, "/api/provider-limit-policies/"+created.ID, owner.AccessToken, owner.OrganizationID, map[string]any{
		"taskType":          "video.create_task",
		"maxConcurrency":    1,
		"requestsPerMinute": 5,
		"enabled":           false,
	}, &updated)
	if updated.Enabled || updated.MaxConcurrency == nil || *updated.MaxConcurrency != 1 {
		t.Fatalf("updated policy = %+v", updated)
	}

	stateID := insertProviderCircuitStateForAPI(t, ctx, pool, owner.OrganizationID, account.ID, model.ID)
	assertAPIErrorCode(t, server, http.MethodPost, "/api/provider-circuit-states/"+stateID+"/reset", member.AccessToken, owner.OrganizationID, nil, http.StatusForbidden, "ACCESS_DENIED")
	var reset provider.ProviderCircuitState
	doAPISuccess(t, server, http.MethodPost, "/api/provider-circuit-states/"+stateID+"/reset", owner.AccessToken, owner.OrganizationID, nil, &reset)
	if reset.State != "closed" || reset.FailureCount != 0 {
		t.Fatalf("reset circuit = %+v", reset)
	}

	doAPISuccess(t, server, http.MethodDelete, "/api/provider-limit-policies/"+created.ID, owner.AccessToken, owner.OrganizationID, nil, &map[string]bool{})
}

func insertProviderCircuitStateForAPI(t *testing.T, ctx context.Context, pool dbQueryer, orgID, accountID, modelID string) string {
	t.Helper()
	var stateID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO provider_circuit_states(
			organization_id, provider_account_id, provider_model_id, task_type,
			state, failure_count, success_count, opened_at, next_attempt_at,
			last_error_code, last_error_message
		)
		VALUES ($1, $2, $3, 'video.create_task', 'open', 3, 0, now(), now() + interval '1 minute', 'UPSTREAM_INTERNAL_ERROR', 'boom')
		RETURNING id::text
	`, orgID, accountID, modelID).Scan(&stateID); err != nil {
		t.Fatalf("insert circuit state: %v", err)
	}
	return stateID
}
