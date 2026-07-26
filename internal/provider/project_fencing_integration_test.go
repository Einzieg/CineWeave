package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Einzieg/cineweave/internal/db"
)

func TestGatewayVideoCreateFencesDeletingProjectBeforeUpstreamIntegration(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run provider project fencing tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for provider project fencing tests")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)

	var upstreamCalls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		http.Error(w, "unexpected upstream call", http.StatusInternalServerError)
	}))
	defer upstream.Close()
	vault, err := NewVault("")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	organizationID, _, projectID, modelID := seedGatewayVideoIntegrationData(t, ctx, pool, vault, upstream.URL)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, organizationID)
	})
	if _, err := pool.Exec(ctx, `
		UPDATE projects
		SET lifecycle_status = 'deleting',
		    deletion_revision = 1,
		    deletion_requested_at = now()
		WHERE id = $1
	`, projectID); err != nil {
		t.Fatalf("mark project deleting: %v", err)
	}

	service := NewService(pool, vault)
	service.EnableGatewayRuntime()
	_, err = service.CreateVideoTask(ctx, GatewayVideoCreateTaskRequest{
		OrganizationID:  organizationID,
		ProjectID:       projectID,
		ProviderModelID: modelID,
		Input:           mustJSON(map[string]any{"prompt": "must not reach upstream"}),
	})
	var standard *StandardErrorError
	if !errors.As(err, &standard) || standard.Standard.Code != CodeProjectDeletionInProgress {
		t.Fatalf("create video error = %v, want %s", err, CodeProjectDeletionInProgress)
	}
	if calls := upstreamCalls.Load(); calls != 0 {
		t.Fatalf("upstream calls = %d, want 0", calls)
	}
	var taskCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM provider_async_tasks WHERE project_id = $1
	`, projectID).Scan(&taskCount); err != nil {
		t.Fatalf("count provider tasks: %v", err)
	}
	if taskCount != 0 {
		t.Fatalf("provider task count = %d, want 0", taskCount)
	}
}
