package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Einzieg/cineweave/internal/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestGatewayProviderLimitIntegration(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run provider gateway limit integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for provider gateway limit integration tests")
	}

	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer pool.Close()

	vault, err := NewVault("")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	upstream := httptest.NewServer(http.NotFoundHandler())
	defer upstream.Close()

	t.Run("image concurrency block", func(t *testing.T) {
		orgID, _, projectID, modelID := seedGatewayImageIntegrationData(t, ctx, pool, vault, upstream.URL)
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
		})
		accountID := providerAccountIDForModel(t, ctx, pool, modelID)
		insertLimitPolicy(t, ctx, pool, orgID, accountID, modelID, TaskTypeImageGenerate, map[string]any{"max_concurrency": 1})
		if _, err := pool.Exec(ctx, `
			INSERT INTO provider_leases(
				organization_id, provider_account_id, provider_model_id,
				task_type, status, acquired_by_service, lease_token, expires_at
			)
			VALUES ($1, $2, $3, $4, 'active', 'test', $5, now() + interval '1 minute')
		`, orgID, accountID, modelID, TaskTypeImageGenerate, "busy-image-"+strings.ReplaceAll(modelID, "-", "")); err != nil {
			t.Fatalf("insert active lease: %v", err)
		}

		service := NewService(pool, vault)
		service.EnableGatewayRuntime()
		service.SetStorage(newMemoryObjectStorage())
		resp, err := service.GenerateImage(ctx, GatewayImageRequest{
			OrganizationID:  orgID,
			ProjectID:       projectID,
			ProviderModelID: modelID,
			Input:           mustJSON(map[string]any{"prompt": "blocked image", "size": "1024x1024"}),
		})
		if err != nil {
			t.Fatalf("generate image: %v", err)
		}
		assertBlockedGatewayResponse(t, resp.Status, resp.Error, CodeProviderConcurrencyLimited)
		assertBlockedCallLog(t, ctx, pool, resp.ProviderCallID, TaskTypeImageGenerate, CodeProviderConcurrencyLimited)
		assertNoCostRecord(t, ctx, pool, resp.ProviderCallID)
	})

	t.Run("text daily budget block", func(t *testing.T) {
		orgID, _, modelID := seedGatewayIntegrationData(t, ctx, pool, vault, upstream.URL)
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
		})
		accountID := providerAccountIDForModel(t, ctx, pool, modelID)
		insertLimitPolicy(t, ctx, pool, orgID, accountID, modelID, TaskTypeTextGenerate, map[string]any{"daily_budget": "0.00000000"})

		service := NewService(pool, vault)
		service.EnableGatewayRuntime()
		resp, err := service.GenerateText(ctx, GatewayTextRequest{
			OrganizationID:  orgID,
			ProviderModelID: modelID,
			Input:           mustJSON(map[string]any{"prompt": "blocked text"}),
		})
		if err != nil {
			t.Fatalf("generate text: %v", err)
		}
		assertBlockedGatewayResponse(t, resp.Status, resp.Error, CodeProviderDailyQuotaExceeded)
		assertBlockedCallLog(t, ctx, pool, resp.ProviderCallID, TaskTypeTextGenerate, CodeProviderDailyQuotaExceeded)
		assertNoCostRecord(t, ctx, pool, resp.ProviderCallID)
	})
}

func assertBlockedGatewayResponse(t *testing.T, status string, standard *StandardError, code string) {
	t.Helper()
	if status != "blocked" {
		t.Fatalf("status = %s, want blocked", status)
	}
	if standard == nil || standard.Code != code {
		t.Fatalf("error = %+v, want code %s", standard, code)
	}
}

func assertBlockedCallLog(t *testing.T, ctx context.Context, pool *pgxpool.Pool, providerCallID, taskType, code string) {
	t.Helper()
	var status, gotTaskType, errorCode string
	if err := pool.QueryRow(ctx, `
		SELECT status, task_type, error_code
		FROM provider_call_logs
		WHERE id = $1
	`, providerCallID).Scan(&status, &gotTaskType, &errorCode); err != nil {
		t.Fatalf("select blocked call log: %v", err)
	}
	if status != "blocked" || gotTaskType != taskType || errorCode != code {
		t.Fatalf("blocked call = status=%s taskType=%s code=%s, want blocked/%s/%s", status, gotTaskType, errorCode, taskType, code)
	}
}

func assertNoCostRecord(t *testing.T, ctx context.Context, pool *pgxpool.Pool, providerCallID string) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM cost_records WHERE provider_call_id = $1`, providerCallID).Scan(&count); err != nil {
		t.Fatalf("select cost_records: %v", err)
	}
	if count != 0 {
		t.Fatalf("cost_records count = %d, want 0", count)
	}
}
