package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/db"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type gatewayVideoIntegrationIdentity struct {
	GenerationID    string
	BindingID       string
	BindingRevision int64
}

func TestGatewayVideoRuntimeIntegration(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run provider gateway video integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for provider gateway video integration tests")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)

	upstream := newVideoRuntimeMock(t)
	defer upstream.Close()

	vault, err := NewVault("")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	orgID, userID, projectID, modelID := seedGatewayVideoIntegrationData(t, ctx, pool, vault, upstream.URL)
	identity := loadGatewayVideoIntegrationIdentity(t, ctx, pool, projectID)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
	})

	objectStorage := newMemoryObjectStorage()
	gatewayService := NewService(pool, vault)
	gatewayService.EnableGatewayRuntime()
	gatewayService.SetStorage(objectStorage)
	gatewayService.SetVideoMediaProbe(func(context.Context, []byte, string) (GatewayVideoMediaProbe, error) {
		return GatewayVideoMediaProbe{
			DurationSeconds:      4.96,
			Width:                1280,
			Height:               720,
			FrameRateNumerator:   24,
			FrameRateDenominator: 1,
			FrameRate:            24,
			FrameCount:           119,
			FrameCountEstimated:  true,
			VideoStreamCount:     1,
			AudioStreamCount:     1,
			HasAudio:             true,
			VideoCodec:           "h264",
			AudioCodecs:          []string{"aac"},
		}, nil
	})

	createResp, err := gatewayService.CreateVideoTask(ctx, GatewayVideoCreateTaskRequest{
		OrganizationID: orgID, ProjectID: projectID, ProviderModelID: modelID,
		ProductionGenerationID: identity.GenerationID, VideoProductionBindingID: identity.BindingID,
		VideoProductionBindingRevision: identity.BindingRevision,
		Input: mustJSON(map[string]any{
			"prompt":      "A cinematic sunrise train station with slow camera movement",
			"duration":    5,
			"aspectRatio": "16:9",
			"resolution":  "720p",
		}),
	})
	if err != nil {
		t.Fatalf("CreateVideoTask: %v", err)
	}
	if createResp.Status != "running" || createResp.ProviderAsyncTaskID == "" || createResp.ExternalTaskID == "" {
		t.Fatalf("create response = %+v", createResp)
	}
	assertGatewayVideoAsyncTask(t, ctx, pool, createResp.ProviderAsyncTaskID, "running", 0)
	assertGatewayVideoCallLog(t, ctx, pool, createResp.ProviderCallID, "video.create_task", "", "")

	firstPoll, err := gatewayService.PollVideoTask(ctx, GatewayVideoPollTaskRequest{
		OrganizationID:         orgID,
		ProviderAsyncTaskID:    createResp.ProviderAsyncTaskID,
		ProjectID:              projectID,
		ProductionGenerationID: identity.GenerationID, VideoProductionBindingID: identity.BindingID,
		VideoProductionBindingRevision: identity.BindingRevision,
	})
	if err != nil {
		t.Fatalf("first PollVideoTask: %v", err)
	}
	if firstPoll.Status != "running" || firstPoll.Output.ArtifactID != "" {
		t.Fatalf("first poll = %+v", firstPoll)
	}
	assertGatewayVideoArtifactCount(t, ctx, pool, createResp.ProviderAsyncTaskID, 0)

	secondPoll, err := gatewayService.PollVideoTask(ctx, GatewayVideoPollTaskRequest{
		OrganizationID:         orgID,
		ProviderAsyncTaskID:    createResp.ProviderAsyncTaskID,
		ProjectID:              projectID,
		ProductionGenerationID: identity.GenerationID, VideoProductionBindingID: identity.BindingID,
		VideoProductionBindingRevision: identity.BindingRevision,
		Options:                        GatewayVideoOptions{IdempotencyKey: "integration-final-poll"},
	})
	if err != nil {
		t.Fatalf("second PollVideoTask: %v", err)
	}
	if secondPoll.Status != "succeeded" || secondPoll.Output.ArtifactID == "" || secondPoll.Output.MediaFileID == "" || secondPoll.Output.StorageKey == "" {
		t.Fatalf("second poll = %+v", secondPoll)
	}
	if secondPoll.Output.RequestedDurationSeconds == nil || *secondPoll.Output.RequestedDurationSeconds != 5 || secondPoll.Output.ActualDurationSeconds == nil || *secondPoll.Output.ActualDurationSeconds != 4.96 || secondPoll.Output.DurationSource != "media_probe" {
		t.Fatalf("duration observation = %+v", secondPoll.Output)
	}
	assertGatewayVideoObjectStored(t, objectStorage, secondPoll.Output.StorageKey)
	assertGatewayVideoRowsPersisted(t, ctx, pool, secondPoll.ProviderCallID, createResp.ProviderAsyncTaskID, createResp.ExternalTaskID, secondPoll.Output, projectID, modelID)
	assertGatewayVideoAsyncTask(t, ctx, pool, createResp.ProviderAsyncTaskID, "succeeded", 2)
	assertGatewayVideoCostRecord(t, ctx, pool, secondPoll.ProviderCallID)
	assertGatewayVideoCallLog(t, ctx, pool, secondPoll.ProviderCallID, "video.poll_task", secondPoll.Output.ArtifactID, secondPoll.Output.MediaFileID)

	if _, err := pool.Exec(ctx, `DELETE FROM media_files WHERE id = $1`, secondPoll.Output.MediaFileID); err != nil {
		t.Fatalf("delete materialized media row: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM artifacts WHERE id = $1`, secondPoll.Output.ArtifactID); err != nil {
		t.Fatalf("delete materialized artifact row: %v", err)
	}
	repairedPoll, err := gatewayService.PollVideoTask(ctx, GatewayVideoPollTaskRequest{
		OrganizationID:         orgID,
		ProviderAsyncTaskID:    createResp.ProviderAsyncTaskID,
		ProjectID:              projectID,
		ProductionGenerationID: identity.GenerationID, VideoProductionBindingID: identity.BindingID,
		VideoProductionBindingRevision: identity.BindingRevision,
		Options:                        GatewayVideoOptions{IdempotencyKey: "integration-final-poll"},
	})
	if err != nil {
		t.Fatalf("repair PollVideoTask: %v", err)
	}
	if repairedPoll.Status != "succeeded" || repairedPoll.Output.ArtifactID == "" || repairedPoll.Output.ArtifactID == secondPoll.Output.ArtifactID {
		t.Fatalf("repaired poll = %+v", repairedPoll)
	}
	assertGatewayVideoRowsPersisted(t, ctx, pool, repairedPoll.ProviderCallID, createResp.ProviderAsyncTaskID, createResp.ExternalTaskID, repairedPoll.Output, projectID, modelID)

	gatewayToken := "video-integration-token"
	gateway := httptest.NewServer(testProviderGatewayHTTP(t, gatewayService, gatewayToken))
	defer gateway.Close()
	apiService := NewService(pool, vault)
	apiService.SetGateway(gateway.URL, gatewayToken)
	result, err := apiService.RecordProviderModelTest(ctx, orgID, userID, modelID, TestProviderModelRequest{
		TestType: "video_generation_test",
		Input: mustJSON(map[string]any{
			"prompt":      "A cinematic sunrise train station with slow camera movement",
			"duration":    5,
			"aspectRatio": "16:9",
			"resolution":  "720p",
			"projectId":   projectID,
		}),
	})
	if err != nil {
		t.Fatalf("video_generation_test: %v", err)
	}
	assertGatewayVideoProviderTestResult(t, result)
	assertSnapshotsDoNotLeakAPIKey(t, ctx, pool, result.ProviderCallID, result.TestRunID)
}

func TestGatewayVideoPollKeepsTaskCredentialAfterRotation(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run provider gateway video integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for provider gateway video integration tests")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)

	upstream := newVideoRuntimeMock(t)
	defer upstream.Close()
	vault, err := NewVault("")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	orgID, userID, projectID, modelID := seedGatewayVideoIntegrationData(t, ctx, pool, vault, upstream.URL)
	identity := loadGatewayVideoIntegrationIdentity(t, ctx, pool, projectID)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID) })

	var accountID, originalCredentialID string
	if err := pool.QueryRow(ctx, `
		SELECT account.id::text, credential.id::text
		FROM provider_models model
		JOIN provider_accounts account ON account.id = model.provider_account_id
		JOIN provider_credential_models availability ON availability.provider_model_id = model.id AND availability.is_available = true
		JOIN provider_credentials credential ON credential.id = availability.provider_credential_id
		WHERE model.id = $1 AND credential.is_active = true AND credential.status = 'active'
		ORDER BY credential.created_at, credential.id
		LIMIT 1
	`, modelID).Scan(&accountID, &originalCredentialID); err != nil {
		t.Fatalf("load original model credential: %v", err)
	}

	service := NewService(pool, vault)
	service.EnableGatewayRuntime()
	service.SetStorage(newMemoryObjectStorage())
	service.SetVideoMediaProbe(func(context.Context, []byte, string) (GatewayVideoMediaProbe, error) {
		return GatewayVideoMediaProbe{
			DurationSeconds: 5, Width: 1280, Height: 720,
			FrameRateNumerator: 24, FrameRateDenominator: 1, FrameRate: 24,
			FrameCount: 120, VideoStreamCount: 1, AudioStreamCount: 1, HasAudio: true,
		}, nil
	})
	created, err := service.CreateVideoTask(ctx, GatewayVideoCreateTaskRequest{
		OrganizationID: orgID, ProjectID: projectID, ProviderModelID: modelID,
		ProductionGenerationID: identity.GenerationID, VideoProductionBindingID: identity.BindingID,
		VideoProductionBindingRevision: identity.BindingRevision,
		Input: mustJSON(map[string]any{
			"prompt": "Credential stickiness integration", "duration": 5,
			"aspectRatio": "16:9", "resolution": "720p",
		}),
	})
	if err != nil || created.ProviderAsyncTaskID == "" {
		t.Fatalf("create video task: response=%+v err=%v", created, err)
	}
	var taskCredentialID string
	if err := pool.QueryRow(ctx, `SELECT credential_id::text FROM provider_async_tasks WHERE id = $1`, created.ProviderAsyncTaskID).Scan(&taskCredentialID); err != nil {
		t.Fatalf("load task credential: %v", err)
	}
	if taskCredentialID != originalCredentialID {
		t.Fatalf("task credential = %s, want %s", taskCredentialID, originalCredentialID)
	}
	rotated, err := service.RotateCredentialByID(ctx, orgID, accountID, originalCredentialID, userID, RotateCredentialRequest{
		Credential: map[string]any{"apiKey": "test-rotated-credential"},
	})
	if err != nil {
		t.Fatalf("rotate task credential: %v", err)
	}
	if rotated.ID == originalCredentialID {
		t.Fatal("credential rotation did not create a new credential identity")
	}

	var poll GatewayVideoPollTaskResponse
	for attempt := 0; attempt < 2; attempt++ {
		poll, err = service.PollVideoTask(ctx, GatewayVideoPollTaskRequest{
			OrganizationID: orgID, ProviderAsyncTaskID: created.ProviderAsyncTaskID,
			ProjectID: projectID, ProductionGenerationID: identity.GenerationID,
			VideoProductionBindingID: identity.BindingID, VideoProductionBindingRevision: identity.BindingRevision,
			Options: GatewayVideoOptions{IdempotencyKey: fmt.Sprintf("credential-stickiness-poll-%d", attempt+1)},
		})
		if err != nil {
			t.Fatalf("poll after rotation attempt %d: %v", attempt+1, err)
		}
	}
	if poll.Status != "succeeded" {
		t.Fatalf("poll after rotation = %+v", poll)
	}
	var pollCredentialID string
	if err := pool.QueryRow(ctx, `
		SELECT credential_id::text FROM provider_call_logs
		WHERE id = $1 AND task_type = 'video.poll_task'
	`, poll.ProviderCallID).Scan(&pollCredentialID); err != nil {
		t.Fatalf("load poll call credential: %v", err)
	}
	if pollCredentialID != originalCredentialID {
		t.Fatalf("poll credential = %s, want original task credential %s", pollCredentialID, originalCredentialID)
	}
}

func TestProviderVideoGenerationTestRejectsCrossOrganizationProjectBeforeGateway(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run provider gateway video integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for provider gateway video integration tests")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)

	upstream := newVideoRuntimeMock(t)
	defer upstream.Close()
	vault, err := NewVault("")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	callerOrganizationID, callerUserID, _, callerModelID := seedGatewayVideoIntegrationData(t, ctx, pool, vault, upstream.URL)
	foreignOrganizationID, _, foreignProjectID, _ := seedGatewayVideoIntegrationData(t, ctx, pool, vault, upstream.URL)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id IN ($1, $2)`, callerOrganizationID, foreignOrganizationID)
	})

	var gatewayMu sync.Mutex
	gatewayCalls := 0
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		gatewayMu.Lock()
		gatewayCalls++
		gatewayMu.Unlock()
		http.Error(w, "gateway must not be called", http.StatusInternalServerError)
	}))
	defer gateway.Close()

	service := NewService(pool, vault)
	service.SetGateway(gateway.URL, "cross-organization-test-token")
	_, err = service.RecordProviderModelTest(ctx, callerOrganizationID, callerUserID, callerModelID, TestProviderModelRequest{
		TestType: "video_generation_test",
		Input: mustJSON(map[string]any{
			"projectId": foreignProjectID,
			"prompt":    "This cross-organization probe must stop before Gateway",
		}),
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("RecordProviderModelTest error = %v, want pgx.ErrNoRows", err)
	}
	gatewayMu.Lock()
	callCount := gatewayCalls
	gatewayMu.Unlock()
	if callCount != 0 {
		t.Fatalf("gateway calls = %d, want 0", callCount)
	}
}

func TestGatewayVideoTaskRejectsLockedProductionGeneration(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run provider gateway video integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for provider gateway video integration tests")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)

	upstream := newVideoRuntimeMock(t)
	defer upstream.Close()
	vault, err := NewVault("")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	orgID, _, projectID, modelID := seedGatewayVideoIntegrationData(t, ctx, pool, vault, upstream.URL)
	identity := loadGatewayVideoIntegrationIdentity(t, ctx, pool, projectID)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
	})

	service := NewService(pool, vault)
	service.EnableGatewayRuntime()
	service.SetStorage(newMemoryObjectStorage())
	created, err := service.CreateVideoTask(ctx, GatewayVideoCreateTaskRequest{
		OrganizationID: orgID, ProjectID: projectID, ProviderModelID: modelID,
		ProductionGenerationID: identity.GenerationID, VideoProductionBindingID: identity.BindingID,
		VideoProductionBindingRevision: identity.BindingRevision,
		Input: mustJSON(map[string]any{
			"prompt": "A stable execution fence test", "duration": 5,
			"aspectRatio": "16:9", "resolution": "720p",
		}),
	})
	if err != nil {
		t.Fatalf("create video task: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE projects SET video_production_locked = true, updated_at = now() WHERE id = $1
	`, projectID); err != nil {
		t.Fatalf("lock production generation: %v", err)
	}
	_, err = service.PollVideoTask(ctx, GatewayVideoPollTaskRequest{
		OrganizationID: orgID, ProjectID: projectID, ProviderAsyncTaskID: created.ProviderAsyncTaskID,
		ProductionGenerationID: identity.GenerationID, VideoProductionBindingID: identity.BindingID,
		VideoProductionBindingRevision: identity.BindingRevision,
	})
	var standard *StandardErrorError
	if !errors.As(err, &standard) || standard.Standard.Code != CodeProductionGenerationMismatch {
		t.Fatalf("stale poll error = %v, want %s", err, CodeProductionGenerationMismatch)
	}
	var status string
	var pollCount int
	if err := pool.QueryRow(ctx, `SELECT status, poll_count FROM provider_async_tasks WHERE id = $1`, created.ProviderAsyncTaskID).Scan(&status, &pollCount); err != nil {
		t.Fatalf("reload provider task: %v", err)
	}
	if status != "running" || pollCount != 0 {
		t.Fatalf("stale poll mutated provider task: status=%s pollCount=%d", status, pollCount)
	}
}

func TestGatewayVideoTaskRejectsInvalidRenderPlanBeforeUpstreamOrAccounting(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run provider gateway video integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for provider gateway video integration tests")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)

	var upstreamMu sync.Mutex
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamMu.Lock()
		upstreamCalls++
		upstreamMu.Unlock()
		http.Error(w, "unexpected upstream call", http.StatusInternalServerError)
	}))
	defer upstream.Close()
	vault, err := NewVault("")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	orgID, _, projectID, modelID := seedGatewayVideoIntegrationData(t, ctx, pool, vault, upstream.URL)
	identity := loadGatewayVideoIntegrationIdentity(t, ctx, pool, projectID)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID) })

	service := NewService(pool, vault)
	service.EnableGatewayRuntime()
	service.SetStorage(newMemoryObjectStorage())
	requestedExecutionPlanID := uuid.NewString()
	requestedRenderSegmentID := uuid.NewString()
	response, err := service.CreateVideoTask(ctx, GatewayVideoCreateTaskRequest{
		OrganizationID: orgID, ProjectID: projectID, ProviderModelID: modelID,
		ProductionGenerationID: identity.GenerationID, VideoProductionBindingID: identity.BindingID,
		VideoProductionBindingRevision: identity.BindingRevision,
		StoryboardShotID:               uuid.NewString(), ExecutionPlanID: requestedExecutionPlanID, RenderSegmentID: requestedRenderSegmentID,
		CapabilitySnapshotHash: strings.Repeat("a", 64),
		Input: mustJSON(map[string]any{
			"prompt": "A request that must be rejected before provider execution", "duration": 5,
			"aspectRatio": "16:9", "resolution": "720p",
		}),
	})
	if err != nil {
		t.Fatalf("CreateVideoTask returned transport error: %v", err)
	}
	if response.Status != "failed" || response.Error == nil || response.Error.Code != CodeRenderPlanReplanRequired {
		t.Fatalf("response = %+v, want failed %s", response, CodeRenderPlanReplanRequired)
	}
	upstreamMu.Lock()
	callCount := upstreamCalls
	upstreamMu.Unlock()
	if callCount != 0 {
		t.Fatalf("upstream calls = %d, want 0", callCount)
	}
	var providerRequests, asyncTasks, callLogs, costRecords int
	if err := pool.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM provider_requests WHERE project_id = $1 AND task_type = 'video.create_task'),
		  (SELECT count(*) FROM provider_async_tasks WHERE project_id = $1),
		  (SELECT count(*) FROM provider_call_logs WHERE project_id = $1),
		  (SELECT count(*) FROM cost_records WHERE project_id = $1)
	`, projectID).Scan(&providerRequests, &asyncTasks, &callLogs, &costRecords); err != nil {
		t.Fatalf("count rejected request side effects: %v", err)
	}
	if providerRequests != 1 || asyncTasks != 0 || callLogs != 0 || costRecords != 0 {
		t.Fatalf("side effects = requests:%d tasks:%d logs:%d costs:%d", providerRequests, asyncTasks, callLogs, costRecords)
	}
	var requestStatus, requestedPlanID, requestedSegmentID string
	if err := pool.QueryRow(ctx, `
		SELECT status, video_render_plan_id::text, video_render_segment_id::text
		FROM provider_requests
		WHERE project_id = $1 AND task_type = 'video.create_task'
	`, projectID).Scan(&requestStatus, &requestedPlanID, &requestedSegmentID); err != nil {
		t.Fatalf("load rejected provider request identity: %v", err)
	}
	if requestStatus != "failed" || requestedPlanID != requestedExecutionPlanID || requestedSegmentID != requestedRenderSegmentID {
		t.Fatalf("rejected provider request identity = status:%s plan:%s segment:%s response:%+v", requestStatus, requestedPlanID, requestedSegmentID, response)
	}
}

func TestGatewayVideoTaskRejectsCrossOrganizationProductionIdentityBeforeUpstream(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run provider gateway video integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for provider gateway video integration tests")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)

	var upstreamMu sync.Mutex
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamMu.Lock()
		upstreamCalls++
		upstreamMu.Unlock()
		http.Error(w, "unexpected upstream call", http.StatusInternalServerError)
	}))
	defer upstream.Close()
	vault, err := NewVault("")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	callerOrgID, _, _, callerModelID := seedGatewayVideoIntegrationData(t, ctx, pool, vault, upstream.URL)
	_, _, foreignProjectID, _ := seedGatewayVideoIntegrationData(t, ctx, pool, vault, upstream.URL)
	foreignIdentity := loadGatewayVideoIntegrationIdentity(t, ctx, pool, foreignProjectID)

	service := NewService(pool, vault)
	service.EnableGatewayRuntime()
	service.SetStorage(newMemoryObjectStorage())
	_, err = service.CreateVideoTask(ctx, GatewayVideoCreateTaskRequest{
		OrganizationID: callerOrgID, ProjectID: foreignProjectID, ProviderModelID: callerModelID,
		ProductionGenerationID: foreignIdentity.GenerationID, VideoProductionBindingID: foreignIdentity.BindingID,
		VideoProductionBindingRevision: foreignIdentity.BindingRevision,
		Input: mustJSON(map[string]any{
			"prompt": "This request must not cross organization boundaries", "duration": 5,
			"aspectRatio": "16:9", "resolution": "720p",
		}),
	})
	var standard *StandardErrorError
	if !errors.As(err, &standard) || standard.Standard.Code != CodeProductionGenerationMismatch {
		t.Fatalf("CreateVideoTask error = %v, want %s", err, CodeProductionGenerationMismatch)
	}
	upstreamMu.Lock()
	callCount := upstreamCalls
	upstreamMu.Unlock()
	if callCount != 0 {
		t.Fatalf("upstream calls = %d, want 0", callCount)
	}
	var providerRequests, asyncTasks, callLogs, costRecords int
	if err := pool.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM provider_requests WHERE organization_id = $1 AND project_id = $2),
		  (SELECT count(*) FROM provider_async_tasks WHERE organization_id = $1 AND project_id = $2),
		  (SELECT count(*) FROM provider_call_logs WHERE organization_id = $1 AND project_id = $2),
		  (SELECT count(*) FROM cost_records WHERE organization_id = $1 AND project_id = $2)
	`, callerOrgID, foreignProjectID).Scan(&providerRequests, &asyncTasks, &callLogs, &costRecords); err != nil {
		t.Fatalf("count rejected request side effects: %v", err)
	}
	if providerRequests != 0 || asyncTasks != 0 || callLogs != 0 || costRecords != 0 {
		t.Fatalf("side effects = requests:%d tasks:%d logs:%d costs:%d, want all zero", providerRequests, asyncTasks, callLogs, costRecords)
	}
}

func newVideoRuntimeMock(t *testing.T) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	polls := map[string]int{}
	durations := map[string]float64{}
	nextTask := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/files/video.mp4" {
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("\x00\x00\x00\x18ftypmp42fake mp4 bytes"))
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+gatewayIntegrationAPIKey {
			t.Errorf("Authorization header = %q, want bearer test key", r.Header.Get("Authorization"))
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/video/create":
			var request map[string]any
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode create request: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			requestedDuration, _ := request["duration"].(float64)
			if request["model"] != "video-integration-model" || request["prompt"] == "" || requestedDuration <= 0 {
				t.Errorf("create request = %#v", request)
			}
			mu.Lock()
			nextTask++
			taskID := fmt.Sprintf("task-%d", nextTask)
			polls[taskID] = 0
			durations[taskID] = requestedDuration
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"taskId": taskID, "status": "processing"})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/video/poll/"):
			taskID := strings.TrimPrefix(r.URL.Path, "/video/poll/")
			mu.Lock()
			polls[taskID]++
			count := polls[taskID]
			duration := durations[taskID]
			mu.Unlock()
			if count == 1 {
				_ = json.NewEncoder(w).Encode(map[string]any{"status": "processing"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":          "completed",
				"videoUrl":        server.URL + "/files/video.mp4",
				"mimeType":        "video/mp4",
				"durationSeconds": duration,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	return server
}

func seedGatewayVideoIntegrationData(t *testing.T, ctx context.Context, pool *pgxpool.Pool, vault *Vault, upstreamURL string) (string, string, string, string) {
	t.Helper()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	var orgID, userID, workspaceID, projectID, connectorID, accountID, credentialID, modelID string
	if err := pool.QueryRow(ctx, `INSERT INTO organizations(name, slug) VALUES ($1, $2) RETURNING id`, "Gateway Video Integration", "gateway-video-integration-"+suffix).Scan(&orgID); err != nil {
		t.Fatalf("insert organization: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users(email, display_name) VALUES ($1, $2) RETURNING id`, "gateway-video-"+suffix+"@example.test", "Gateway Video Test").Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
		if connectorID != "" {
			_, _ = pool.Exec(context.Background(), `DELETE FROM provider_connectors WHERE id = $1`, connectorID)
		}
	})
	if _, err := pool.Exec(ctx, `INSERT INTO organization_members(organization_id, user_id) VALUES ($1, $2)`, orgID, userID); err != nil {
		t.Fatalf("insert organization member: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO workspaces(organization_id, name) VALUES ($1, 'Video Workspace') RETURNING id`, orgID).Scan(&workspaceID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin project seed: %v", err)
	}
	defer tx.Rollback(ctx)
	profileVersion, err := videoproduction.ResolveProfileVersion(ctx, tx, videoproduction.ProfileSingleFrameI2V, nil, true)
	if err != nil {
		t.Fatalf("resolve video production profile: %v", err)
	}
	identity := videoproduction.NewIdentity()
	projectID = identity.ProjectID
	if _, err := tx.Exec(ctx, `
		INSERT INTO projects(
			id, organization_id, workspace_id, name, created_by,
			active_video_production_generation_id, video_production_generation_no,
			video_production_state, video_production_locked,
			video_ratio, audio_strategy, audio_requirement
		)
		VALUES ($1, $2, $3, 'Video Project', $4, $5, 1, 'storyboard_required', false, '16:9', 'native_av', 'preferred')
	`, projectID, orgID, workspaceID, userID, identity.GenerationID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO project_members(project_id, user_id) VALUES ($1, $2)`, projectID, userID); err != nil {
		t.Fatalf("insert project member: %v", err)
	}
	if _, _, err := videoproduction.CreateInitialBindingAndGeneration(ctx, tx, videoproduction.InitialBindingParams{
		Identity: identity, OrganizationID: orgID, CreatedBy: userID, ProfileVersion: profileVersion,
		CompatibilityPolicy: videoproduction.CompatibilityStrict,
		Configuration: videoproduction.ProductionConfigurationSnapshot{
			VideoRatio: "16:9", AudioStrategy: "native_av", AudioRequirement: "preferred", ImageQuality: "standard",
		},
	}); err != nil {
		t.Fatalf("create video production context: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit project seed: %v", err)
	}
	manifest := videoIntegrationManifest(upstreamURL)
	if err := pool.QueryRow(ctx, `
		INSERT INTO provider_connectors(connector_key, name, type, is_official, manifest, version)
		VALUES ($1, 'Video Manifest Integration', 'http', true, $2, 'v1')
		RETURNING id
	`, "video-manifest-integration-"+suffix, manifest).Scan(&connectorID); err != nil {
		t.Fatalf("insert provider connector: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO provider_accounts(organization_id, connector_id, name, base_url, auth_type, status, config, created_by)
		VALUES ($1, $2, 'Video Integration Account', $3, 'bearer', 'active',
		        '{"mediaEgress":{"allowedPrivateHosts":["127.0.0.1"],"allowedPrivateCidrs":["127.0.0.0/8"]}}', $4)
		RETURNING id
	`, orgID, connectorID, upstreamURL, userID).Scan(&accountID); err != nil {
		t.Fatalf("insert provider account: %v", err)
	}
	encrypted, err := vault.EncryptJSON(map[string]any{"apiKey": gatewayIntegrationAPIKey})
	if err != nil {
		t.Fatalf("encrypt credential: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO provider_credentials(
			organization_id, provider_account_id, credential_key, credential_type,
			secret_ref, encrypted_payload, masked_preview, status, is_active, created_by
		)
		VALUES ($1, $2, 'default', 'api_key', 'local:aes-gcm:v1', $3, $4, 'active', true, $5)
		RETURNING id
	`, orgID, accountID, encrypted, MaskSecret(gatewayIntegrationAPIKey), userID).Scan(&credentialID); err != nil {
		t.Fatalf("insert provider credential: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO provider_models(provider_account_id, model_key, display_name, modality, status)
		VALUES ($1, 'video-integration-model', 'Video Integration Model', 'video', 'active')
		RETURNING id
	`, accountID).Scan(&modelID); err != nil {
		t.Fatalf("insert provider model: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO provider_credential_models(
			provider_credential_id, provider_model_id, is_available,
			last_discovered_at
		)
		VALUES ($1, $2, true, now())
	`, credentialID, modelID); err != nil {
		t.Fatalf("insert provider credential model availability: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO provider_model_capabilities(
			provider_model_id, task_types, input_limits, output_limits, quality_tiers, provider_options_schema, pricing_policy
		)
		VALUES ($1, '["video.create_task","video.poll_task","video.cancel_task"]', '{}', '{}', '["standard"]', '{}', '{"currency":"USD","videoCostPerSecond":"0.0500","videoCostByResolution":{"720p":"0.0500"},"videoCostFlat":"0.2000"}')
	`, modelID); err != nil {
		t.Fatalf("insert provider model capability: %v", err)
	}
	return orgID, userID, projectID, modelID
}

func loadGatewayVideoIntegrationIdentity(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	projectID string,
) gatewayVideoIntegrationIdentity {
	t.Helper()
	var identity gatewayVideoIntegrationIdentity
	if err := pool.QueryRow(ctx, `
		SELECT generation.id::text, binding.id::text, binding.revision
		FROM projects project
		JOIN project_video_production_generations generation
		  ON generation.id = project.active_video_production_generation_id AND generation.status = 'active'
		JOIN project_video_production_bindings binding
		  ON binding.id = generation.binding_id AND binding.status = 'active'
		WHERE project.id = $1
	`, projectID).Scan(&identity.GenerationID, &identity.BindingID, &identity.BindingRevision); err != nil {
		t.Fatalf("load video production identity: %v", err)
	}
	return identity
}

func videoIntegrationManifest(baseURL string) json.RawMessage {
	return mustJSON(map[string]any{
		"kind":      "ProviderConnector",
		"version":   "v1",
		"id":        "video-integration",
		"name":      "Video Integration",
		"transport": "http",
		"baseUrl":   baseURL,
		"auth": map[string]any{
			"type":          "bearer",
			"header":        "Authorization",
			"valueTemplate": "Bearer {{ credential.apiKey }}",
		},
		"models": []map[string]any{{
			"id":          "video-integration-model",
			"displayName": "Video Integration Model",
			"modality":    "video",
			"capabilities": map[string]any{
				"taskTypes": []string{"video.generate"},
			},
		}},
		"endpoints": map[string]any{
			"video_generate": map[string]any{
				"endpointType":    "async_create",
				"method":          "POST",
				"pathTemplate":    "/video/create",
				"pollEndpointKey": "video_poll",
				"requestTemplate": map[string]any{
					"model":        "{{ model.id }}",
					"prompt":       "{{ input.prompt }}",
					"duration":     "{{ input.duration }}",
					"aspect_ratio": "{{ input.aspectRatio }}",
					"resolution":   "{{ input.resolution }}",
					"image":        "{{ references[0].url }}",
				},
				"responseMapping": map[string]string{
					"externalTaskId": "$.taskId",
					"status":         "$.status",
				},
			},
			"video_poll": map[string]any{
				"endpointType": "async_poll",
				"method":       "GET",
				"pathTemplate": "/video/poll/{{ task.externalTaskId }}",
				"responseMapping": map[string]string{
					"status":          "$.status",
					"videoUrl":        "$.videoUrl",
					"mimeType":        "$.mimeType",
					"durationSeconds": "$.durationSeconds",
				},
			},
		},
	})
}

func assertGatewayVideoObjectStored(t *testing.T, objectStorage *memoryObjectStorage, storageKey string) {
	t.Helper()
	object, ok := objectStorage.get(storageKey)
	if !ok {
		t.Fatalf("storage key %q was not written", storageKey)
	}
	if object.contentType != "video/mp4" || string(object.body) != "\x00\x00\x00\x18ftypmp42fake mp4 bytes" {
		t.Fatalf("stored object = %+v", object)
	}
}

func assertGatewayVideoAsyncTask(t *testing.T, ctx context.Context, pool *pgxpool.Pool, taskID, wantStatus string, wantPollCount int) {
	t.Helper()
	var status, probeStatus string
	var pollCount int
	var requestedDuration, actualDuration float64
	if err := pool.QueryRow(ctx, `
		SELECT status, poll_count,
		       COALESCE(requested_duration_seconds, -1)::float8,
		       COALESCE(actual_duration_seconds, -1)::float8,
		       COALESCE(media_probe->>'status', '')
		FROM provider_async_tasks
		WHERE id = $1
	`, taskID).Scan(&status, &pollCount, &requestedDuration, &actualDuration, &probeStatus); err != nil {
		t.Fatalf("select provider_async_tasks: %v", err)
	}
	if status != wantStatus || pollCount != wantPollCount {
		t.Fatalf("async task status/poll_count = %s/%d, want %s/%d", status, pollCount, wantStatus, wantPollCount)
	}
	if requestedDuration != 5 {
		t.Fatalf("async task requested duration = %f, want 5", requestedDuration)
	}
	if wantStatus == "succeeded" && (actualDuration != 4.96 || probeStatus != "succeeded") {
		t.Fatalf("async task actual/probe = %f/%s, want 4.96/succeeded", actualDuration, probeStatus)
	}
}

func assertGatewayVideoArtifactCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, taskID string, want int) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM artifacts WHERE metadata->>'providerAsyncTaskId' = $1`, taskID).Scan(&count); err != nil {
		t.Fatalf("count video artifacts: %v", err)
	}
	if count != want {
		t.Fatalf("video artifact count = %d, want %d", count, want)
	}
}

func assertGatewayVideoRowsPersisted(t *testing.T, ctx context.Context, pool *pgxpool.Pool, providerCallID, providerAsyncTaskID, externalTaskID string, output GatewayVideoOutput, projectID, modelID string) {
	t.Helper()
	var mediaArtifactID, mediaStorageKey, mediaMimeType, mediaChecksum, mediaSource, mediaCallID, mediaTaskID, mediaProbeStatus string
	var mediaByteSize int64
	var mediaDuration float64
	var frameRateNumerator, frameRateDenominator, frameCount int64
	var videoStreamCount, audioStreamCount int
	if err := pool.QueryRow(ctx, `
		SELECT artifact_id::text, storage_key, mime_type, byte_size, checksum,
		       metadata->>'source', metadata->>'providerCallId', metadata->>'providerAsyncTaskId',
		       duration_seconds::float8, frame_rate_numerator, frame_rate_denominator, frame_count,
		       video_stream_count, audio_stream_count, media_probe->>'status'
		FROM media_files
		WHERE id = $1
	`, output.MediaFileID).Scan(&mediaArtifactID, &mediaStorageKey, &mediaMimeType, &mediaByteSize, &mediaChecksum, &mediaSource, &mediaCallID, &mediaTaskID, &mediaDuration, &frameRateNumerator, &frameRateDenominator, &frameCount, &videoStreamCount, &audioStreamCount, &mediaProbeStatus); err != nil {
		t.Fatalf("select media_files: %v", err)
	}
	if mediaArtifactID != output.ArtifactID || mediaStorageKey != output.StorageKey || mediaMimeType != "video/mp4" || mediaByteSize == 0 || mediaSource != "provider_gateway" || mediaCallID != providerCallID || mediaTaskID != providerAsyncTaskID || !strings.HasPrefix(mediaChecksum, "sha256:") {
		t.Fatalf("media row mismatch: artifact=%s key=%s mime=%s bytes=%d source=%s call=%s task=%s checksum=%s", mediaArtifactID, mediaStorageKey, mediaMimeType, mediaByteSize, mediaSource, mediaCallID, mediaTaskID, mediaChecksum)
	}
	if mediaDuration != 4.96 || frameRateNumerator != 24 || frameRateDenominator != 1 || frameCount != 119 || videoStreamCount != 1 || audioStreamCount != 1 || mediaProbeStatus != "succeeded" {
		t.Fatalf("media observation mismatch: duration=%f fps=%d/%d frames=%d streams=%d/%d probe=%s", mediaDuration, frameRateNumerator, frameRateDenominator, frameCount, videoStreamCount, audioStreamCount, mediaProbeStatus)
	}

	var artifactProjectID, artifactType, artifactStorageKey, artifactModelID, artifactMediaID, artifactCallID, artifactTaskID, artifactExternalTaskID string
	if err := pool.QueryRow(ctx, `
		SELECT project_id::text, type, storage_key, model_id::text, metadata->>'mediaFileId', metadata->>'providerCallId', metadata->>'providerAsyncTaskId', metadata->>'externalTaskId'
		FROM artifacts
		WHERE id = $1
	`, output.ArtifactID).Scan(&artifactProjectID, &artifactType, &artifactStorageKey, &artifactModelID, &artifactMediaID, &artifactCallID, &artifactTaskID, &artifactExternalTaskID); err != nil {
		t.Fatalf("select artifacts: %v", err)
	}
	if artifactProjectID != projectID || artifactType != "generated_video" || artifactStorageKey != output.StorageKey || artifactModelID != modelID || artifactMediaID != output.MediaFileID || artifactCallID != providerCallID || artifactTaskID != providerAsyncTaskID || artifactExternalTaskID != externalTaskID {
		t.Fatalf("artifact row mismatch: project=%s type=%s key=%s model=%s media=%s call=%s task=%s external=%s", artifactProjectID, artifactType, artifactStorageKey, artifactModelID, artifactMediaID, artifactCallID, artifactTaskID, artifactExternalTaskID)
	}
}

func assertGatewayVideoCostRecord(t *testing.T, ctx context.Context, pool *pgxpool.Pool, providerCallID string) {
	t.Helper()
	var costType, unit, currency, resolution string
	var amount, quantity float64
	if err := pool.QueryRow(ctx, `
		SELECT cost_type, unit, currency, amount::float8, quantity::float8, metadata->>'resolution'
		FROM cost_records
		WHERE provider_call_id = $1
	`, providerCallID).Scan(&costType, &unit, &currency, &amount, &quantity, &resolution); err != nil {
		t.Fatalf("select cost_records: %v", err)
	}
	if costType != "video.generate" || unit != "second" || currency != "USD" || math.Abs(amount-0.248) > 0.0000001 || math.Abs(quantity-4.96) > 0.0000001 || resolution != "720p" {
		t.Fatalf("cost row mismatch: type=%s unit=%s currency=%s amount=%f quantity=%f resolution=%s", costType, unit, currency, amount, quantity, resolution)
	}
}

func assertGatewayVideoCallLog(t *testing.T, ctx context.Context, pool *pgxpool.Pool, providerCallID, wantTaskType, artifactID, mediaFileID string) {
	t.Helper()
	var taskType, artifactIDsRaw, mediaFileIDsRaw, requestSnapshot, probeStatus string
	var requestedDuration, actualDuration float64
	if err := pool.QueryRow(ctx, `
		SELECT task_type, artifact_ids::text, media_file_ids::text, request_snapshot::text,
		       COALESCE(requested_duration_seconds, -1)::float8,
		       COALESCE(actual_duration_seconds, -1)::float8,
		       COALESCE(media_probe->>'status', '')
		FROM provider_call_logs
		WHERE id = $1
	`, providerCallID).Scan(&taskType, &artifactIDsRaw, &mediaFileIDsRaw, &requestSnapshot, &requestedDuration, &actualDuration, &probeStatus); err != nil {
		t.Fatalf("select provider_call_logs: %v", err)
	}
	if taskType != wantTaskType {
		t.Fatalf("taskType = %s, want %s", taskType, wantTaskType)
	}
	if strings.Contains(requestSnapshot, gatewayIntegrationAPIKey) {
		t.Fatal("request_snapshot leaked API key")
	}
	if artifactID != "" && (!jsonArrayContains(artifactIDsRaw, artifactID) || !jsonArrayContains(mediaFileIDsRaw, mediaFileID)) {
		t.Fatalf("call log ids mismatch: artifact_ids=%s media_file_ids=%s", artifactIDsRaw, mediaFileIDsRaw)
	}
	if requestedDuration != 5 {
		t.Fatalf("call log requested duration = %f, want 5", requestedDuration)
	}
	if artifactID != "" && (actualDuration != 4.96 || probeStatus != "succeeded") {
		t.Fatalf("call log actual/probe = %f/%s, want 4.96/succeeded", actualDuration, probeStatus)
	}
}

func assertGatewayVideoProviderTestResult(t *testing.T, result ProviderTestResult) {
	t.Helper()
	if result.Status != "succeeded" {
		t.Fatalf("status = %q, want succeeded; error=%v", result.Status, result.ErrorMessage)
	}
	var output map[string]any
	if err := json.Unmarshal(result.NormalizedOutput, &output); err != nil {
		t.Fatalf("decode normalized output: %v", err)
	}
	if output["providerAsyncTaskId"] == "" || output["artifactId"] == "" || output["mediaFileId"] == "" || output["storageKey"] == "" {
		t.Fatalf("provider test normalized output = %#v", output)
	}
}
