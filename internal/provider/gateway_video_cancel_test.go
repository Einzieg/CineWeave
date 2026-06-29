package provider

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/Einzieg/cineweave/internal/db"
)

func TestGatewayVideoCancelIdempotency(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run provider gateway video cancel tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for provider gateway video cancel tests")
	}
	t.Setenv("CINEWEAVE_ALLOW_PRIVATE_PROVIDER_MEDIA_URLS", "true")

	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer pool.Close()

	upstream := newVideoRuntimeMock(t)
	defer upstream.Close()
	vault, err := NewVault("")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	orgID, _, projectID, modelID := seedGatewayVideoIntegrationData(t, ctx, pool, vault, upstream.URL)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
	})
	service := NewService(pool, vault)
	service.EnableGatewayRuntime()
	service.SetStorage(newMemoryObjectStorage())

	createResp, err := service.CreateVideoTask(ctx, GatewayVideoCreateTaskRequest{
		OrganizationID:  orgID,
		ProjectID:       projectID,
		ProviderModelID: modelID,
		Input:           mustJSON(map[string]any{"prompt": "cancel me", "duration": 5, "aspectRatio": "16:9", "resolution": "720p"}),
	})
	if err != nil {
		t.Fatalf("CreateVideoTask: %v", err)
	}
	cancelResp, err := service.CancelVideoTask(ctx, GatewayVideoCancelTaskRequest{
		OrganizationID:      orgID,
		ProviderAsyncTaskID: createResp.ProviderAsyncTaskID,
	})
	if err != nil {
		t.Fatalf("CancelVideoTask: %v", err)
	}
	if cancelResp.Status != "cancelled" {
		t.Fatalf("cancel response = %+v", cancelResp)
	}
	assertGatewayVideoAsyncTask(t, ctx, pool, createResp.ProviderAsyncTaskID, "cancelled", 0)

	repeated, err := service.CancelVideoTask(ctx, GatewayVideoCancelTaskRequest{
		OrganizationID:      orgID,
		ProviderAsyncTaskID: createResp.ProviderAsyncTaskID,
	})
	if err != nil {
		t.Fatalf("repeated CancelVideoTask: %v", err)
	}
	if repeated.Status != "cancelled" {
		t.Fatalf("repeated cancel response = %+v", repeated)
	}

	completedCreate, err := service.CreateVideoTask(ctx, GatewayVideoCreateTaskRequest{
		OrganizationID:  orgID,
		ProjectID:       projectID,
		ProviderModelID: modelID,
		Input:           mustJSON(map[string]any{"prompt": "complete me", "duration": 5, "aspectRatio": "16:9", "resolution": "720p"}),
	})
	if err != nil {
		t.Fatalf("CreateVideoTask completed: %v", err)
	}
	if _, err := service.PollVideoTask(ctx, GatewayVideoPollTaskRequest{OrganizationID: orgID, ProviderAsyncTaskID: completedCreate.ProviderAsyncTaskID, ProjectID: projectID}); err != nil {
		t.Fatalf("first poll completed: %v", err)
	}
	if _, err := service.PollVideoTask(ctx, GatewayVideoPollTaskRequest{OrganizationID: orgID, ProviderAsyncTaskID: completedCreate.ProviderAsyncTaskID, ProjectID: projectID}); err != nil {
		t.Fatalf("second poll completed: %v", err)
	}
	completedCancel, err := service.CancelVideoTask(ctx, GatewayVideoCancelTaskRequest{
		OrganizationID:      orgID,
		ProviderAsyncTaskID: completedCreate.ProviderAsyncTaskID,
	})
	if err != nil {
		t.Fatalf("CancelVideoTask completed: %v", err)
	}
	if completedCancel.Status != "succeeded" {
		t.Fatalf("completed cancel response = %+v", completedCancel)
	}
	assertGatewayVideoAsyncTask(t, ctx, pool, completedCreate.ProviderAsyncTaskID, "succeeded", 2)
}
