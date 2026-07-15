package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/db"
)

func TestGatewayRequestHashIgnoresExecutionControls(t *testing.T) {
	base := GatewayTextRequest{
		OrganizationID:  "org-1",
		ProviderModelID: "model-1",
		IdempotencyKey:  "request-a",
		Input:           json.RawMessage(`{"messages":[{"role":"user","content":"hello"}]}`),
		Options: GatewayTextOptions{
			IdempotencyKey: "request-a",
			Retry:          false,
		},
	}
	retry := base
	retry.IdempotencyKey = "request-b"
	retry.Options.IdempotencyKey = "request-b"
	retry.Options.Retry = true

	baseHash, err := gatewayRequestHash(base)
	if err != nil {
		t.Fatalf("hash base request: %v", err)
	}
	retryHash, err := gatewayRequestHash(retry)
	if err != nil {
		t.Fatalf("hash retry request: %v", err)
	}
	if baseHash != retryHash {
		t.Fatalf("execution controls changed request hash: %s != %s", baseHash, retryHash)
	}

	changed := base
	changed.Input = json.RawMessage(`{"messages":[{"role":"user","content":"different"}]}`)
	changedHash, err := gatewayRequestHash(changed)
	if err != nil {
		t.Fatalf("hash changed request: %v", err)
	}
	if baseHash == changedHash {
		t.Fatal("semantic request change did not change request hash")
	}
}

func TestGatewayRequestHashPreservesNestedSemanticExecutionNames(t *testing.T) {
	base := GatewayTextRequest{
		OrganizationID:  "org-1",
		ProviderModelID: "model-1",
		Input: json.RawMessage(`{
			"messages":[{"role":"user","content":"hello"}],
			"toolSchema":{"properties":{"retry":{"type":"boolean","default":false}}}
		}`),
	}
	changed := base
	changed.Input = json.RawMessage(`{
		"messages":[{"role":"user","content":"hello"}],
		"toolSchema":{"properties":{"retry":{"type":"boolean","default":true}}}
	}`)

	baseHash, err := gatewayRequestHash(base)
	if err != nil {
		t.Fatalf("hash base request: %v", err)
	}
	changedHash, err := gatewayRequestHash(changed)
	if err != nil {
		t.Fatalf("hash changed request: %v", err)
	}
	if baseHash == changedHash {
		t.Fatal("nested semantic field named retry was removed from the logical request hash")
	}
}

func TestProviderRequestIdempotencyIntegration(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run provider request integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for provider request integration tests")
	}

	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)

	var upstreamCalls atomic.Int64
	requestStarted := make(chan struct{}, 1)
	releaseRequest := make(chan struct{})
	var releaseOnce sync.Once
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		prompt := providerTestPrompt(body)
		if prompt == "block" {
			select {
			case requestStarted <- struct{}{}:
			default:
			}
			<-releaseRequest
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer func() {
		releaseOnce.Do(func() { close(releaseRequest) })
		upstream.Close()
	}()

	vault, err := NewVault("")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	orgID, _, modelID := seedGatewayIntegrationData(t, ctx, pool, vault, upstream.URL)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
	})
	service := NewService(pool, vault)
	service.EnableGatewayRuntime()

	req := GatewayTextRequest{
		OrganizationID:  orgID,
		ProviderModelID: modelID,
		IdempotencyKey:  "dedupe-success",
		Input:           json.RawMessage(`{"messages":[{"role":"user","content":"once"}]}`),
	}
	first, err := service.GenerateText(ctx, req)
	if err != nil {
		t.Fatalf("first generate: %v", err)
	}
	second, err := service.GenerateText(ctx, req)
	if err != nil {
		t.Fatalf("replay generate: %v", err)
	}
	if first.ProviderRequestID == "" || first.ProviderRequestID != second.ProviderRequestID {
		t.Fatalf("provider request ids = %q and %q", first.ProviderRequestID, second.ProviderRequestID)
	}
	if first.ProviderCallID != second.ProviderCallID {
		t.Fatalf("provider call ids = %q and %q", first.ProviderCallID, second.ProviderCallID)
	}
	if got := upstreamCalls.Load(); got != 1 {
		t.Fatalf("upstream calls after replay = %d, want 1", got)
	}
	assertProviderRequestCounts(t, ctx, pool, first.ProviderRequestID, 1, 1)

	conflict := req
	conflict.Input = json.RawMessage(`{"messages":[{"role":"user","content":"changed"}]}`)
	_, err = service.GenerateText(ctx, conflict)
	var standardErr *StandardErrorError
	if !errors.As(err, &standardErr) || standardErr.Standard.Code != CodeProviderIdempotencyConflict {
		t.Fatalf("conflicting request error = %v", err)
	}

	blockingReq := GatewayTextRequest{
		OrganizationID:  orgID,
		ProviderModelID: modelID,
		IdempotencyKey:  "dedupe-concurrent",
		Input:           json.RawMessage(`{"messages":[{"role":"user","content":"block"}]}`),
	}
	resultCh := make(chan GatewayTextResponse, 1)
	errCh := make(chan error, 1)
	go func() {
		response, runErr := service.GenerateText(ctx, blockingReq)
		resultCh <- response
		errCh <- runErr
	}()
	select {
	case <-requestStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream request did not start")
	}

	var runningRequestID, requestStatus, callStatus string
	if err := pool.QueryRow(ctx, `
		SELECT pr.id::text, pr.status, pcl.status
		FROM provider_requests pr
		JOIN provider_call_logs pcl ON pcl.provider_request_id = pr.id
		WHERE pr.organization_id = $1 AND pr.task_type = $2 AND pr.idempotency_key = $3
	`, orgID, TaskTypeTextGenerate, "dedupe-concurrent").Scan(&runningRequestID, &requestStatus, &callStatus); err != nil {
		t.Fatalf("query running request: %v", err)
	}
	if requestStatus != "running" || callStatus != "running" {
		t.Fatalf("prewritten statuses = request %q call %q", requestStatus, callStatus)
	}

	inProgress, err := service.GenerateText(ctx, blockingReq)
	if err != nil {
		t.Fatalf("concurrent request: %v", err)
	}
	if inProgress.Status != "running" || inProgress.ProviderRequestID != runningRequestID {
		t.Fatalf("concurrent response = %#v", inProgress)
	}
	if got := upstreamCalls.Load(); got != 2 {
		t.Fatalf("upstream calls while concurrent = %d, want 2 total", got)
	}

	releaseOnce.Do(func() { close(releaseRequest) })
	if err := <-errCh; err != nil {
		t.Fatalf("blocking generate: %v", err)
	}
	completed := <-resultCh
	if completed.Status != "succeeded" {
		t.Fatalf("blocking status = %q", completed.Status)
	}
	assertProviderRequestCounts(t, ctx, pool, completed.ProviderRequestID, 1, 1)

	staleHash, err := gatewayRequestHash(GatewayTextRequest{
		OrganizationID: orgID,
		Input:          json.RawMessage(`{"messages":[{"role":"user","content":"stale"}]}`),
	})
	if err != nil {
		t.Fatalf("hash stale request: %v", err)
	}
	stale, err := service.beginProviderRequest(ctx, providerRequestStartInput{
		OrganizationID: orgID,
		TaskType:       TaskTypeTextGenerate,
		IdempotencyKey: "stale-request",
		RequestHash:    staleHash,
	})
	if err != nil {
		t.Fatalf("begin stale request: %v", err)
	}
	accountID := providerAccountIDForModel(t, ctx, pool, modelID)
	if _, err := recordCall(ctx, pool, RecordCallRequest{
		ProviderRequestID: stale.Request.ID,
		AttemptGeneration: stale.Request.AttemptGeneration,
		AttemptSequence:   1,
		OrganizationID:    orgID,
		ProviderAccountID: accountID,
		ProviderModelID:   modelID,
		TaskType:          TaskTypeTextGenerate,
		Status:            "running",
		RequestSnapshot:   json.RawMessage(`{"stale":true}`),
	}); err != nil {
		t.Fatalf("prewrite stale call: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE provider_requests SET updated_at = now() - interval '2 hours' WHERE id = $1`, stale.Request.ID); err != nil {
		t.Fatalf("age stale request: %v", err)
	}
	reconciled, err := service.ReconcileStaleProviderRequests(ctx, time.Minute, 10)
	if err != nil || reconciled != 1 {
		t.Fatalf("reconcile stale requests = %d, %v", reconciled, err)
	}
	var staleRequestStatus, staleCallStatus string
	if err := pool.QueryRow(ctx, `
		SELECT pr.status, pcl.status
		FROM provider_requests pr
		JOIN provider_call_logs pcl ON pcl.provider_request_id = pr.id
		WHERE pr.id = $1
	`, stale.Request.ID).Scan(&staleRequestStatus, &staleCallStatus); err != nil {
		t.Fatalf("query reconciled request: %v", err)
	}
	if staleRequestStatus != "unknown_outcome" || staleCallStatus != "unknown_outcome" {
		t.Fatalf("reconciled statuses = %q/%q", staleRequestStatus, staleCallStatus)
	}
	replayedUnknown, err := service.beginProviderRequest(ctx, providerRequestStartInput{
		OrganizationID: orgID, TaskType: TaskTypeTextGenerate, IdempotencyKey: "stale-request", RequestHash: staleHash,
	})
	if err != nil || replayedUnknown.Disposition != providerRequestReplay || replayedUnknown.Request.AttemptGeneration != 1 {
		t.Fatalf("unknown replay = %+v, %v", replayedUnknown, err)
	}
	retried, err := service.beginProviderRequest(ctx, providerRequestStartInput{
		OrganizationID: orgID, TaskType: TaskTypeTextGenerate, IdempotencyKey: "stale-request", RequestHash: staleHash, Retry: true,
	})
	if err != nil || retried.Disposition != providerRequestExecute || retried.Request.AttemptGeneration != 2 {
		t.Fatalf("explicit retry = %+v, %v", retried, err)
	}
}

func TestProviderTextStreamV2RetryAndReplayIdentityIntegration(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run provider request integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for provider request integration tests")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)

	var upstreamCalls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		call := upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		if call == 1 {
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"discarded generation\"}}]}\n\n"))
			return
		}
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"accepted generation\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	vault, err := NewVault("")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	orgID, _, modelID := seedGatewayIntegrationData(t, ctx, pool, vault, upstream.URL)
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID) })
	service := NewService(pool, vault)
	service.EnableGatewayRuntime()
	req := GatewayTextRequest{
		OrganizationID:  orgID,
		ProviderModelID: modelID,
		IdempotencyKey:  "stream-v2-generation-replay",
		Input:           json.RawMessage(`{"messages":[{"role":"user","content":"retry stream"}]}`),
	}

	var failedEvents []GatewayTextStreamEvent
	failedResponse, err := service.StreamTextEvents(ctx, req, func(event GatewayTextStreamEvent) error {
		failedEvents = append(failedEvents, event)
		return nil
	})
	var standardErr *StandardErrorError
	if !errors.As(err, &standardErr) || standardErr.Standard.Code != CodeUpstreamStreamTruncated || failedResponse.AttemptGeneration != 1 {
		t.Fatalf("first stream response=%+v error=%v", failedResponse, err)
	}

	req.Options.Retry = true
	var retriedEvents []GatewayTextStreamEvent
	retried, err := service.StreamTextEvents(ctx, req, func(event GatewayTextStreamEvent) error {
		retriedEvents = append(retriedEvents, event)
		return nil
	})
	if err != nil {
		t.Fatalf("retry stream: %v", err)
	}
	if retried.Status != "succeeded" || retried.AttemptGeneration != 2 || retried.AttemptSequence != 1 || retried.Output.Text != "accepted generation" {
		t.Fatalf("retry response = %+v", retried)
	}
	for _, event := range retriedEvents {
		switch event.Type {
		case GatewayTextEventAttemptStarted:
			if event.Attempt == nil || event.Attempt.AttemptGeneration != 2 || event.Attempt.ProviderRequestID != retried.ProviderRequestID {
				t.Fatalf("retry attempt identity = %+v", event.Attempt)
			}
		case GatewayTextEventDelta:
			if event.Delta == nil || event.Delta.AttemptGeneration != 2 || event.Delta.ProviderRequestID != retried.ProviderRequestID || event.Delta.ProviderCallID != retried.ProviderCallID {
				t.Fatalf("retry delta identity = %+v", event.Delta)
			}
		case GatewayTextEventCompleted:
			if event.Response == nil || event.Response.AttemptGeneration != 2 || event.Response.ProviderCallID != retried.ProviderCallID {
				t.Fatalf("retry completion identity = %+v", event.Response)
			}
		}
	}

	req.Options.Retry = false
	var replayEvents []GatewayTextStreamEvent
	replayed, err := service.StreamTextEvents(ctx, req, func(event GatewayTextStreamEvent) error {
		replayEvents = append(replayEvents, event)
		return nil
	})
	if err != nil {
		t.Fatalf("replay stream: %v", err)
	}
	if len(replayEvents) != 1 || replayEvents[0].Type != GatewayTextEventReplayed || replayEvents[0].Replay == nil {
		t.Fatalf("replay events = %v", gatewayTextEventTypes(replayEvents))
	}
	if replayed.ProviderRequestID != retried.ProviderRequestID || replayed.ProviderCallID != retried.ProviderCallID || replayed.AttemptGeneration != 2 || replayed.Output.Text != retried.Output.Text {
		t.Fatalf("replayed=%+v retried=%+v", replayed, retried)
	}
	if replayEvents[0].Replay.ProviderCallID != retried.ProviderCallID || replayEvents[0].Replay.AttemptGeneration != 2 || upstreamCalls.Load() != 2 {
		t.Fatalf("replay payload=%+v upstreamCalls=%d", replayEvents[0].Replay, upstreamCalls.Load())
	}
	if !hasGatewayTextEvent(failedEvents, GatewayTextEventDelta) || !hasGatewayTextEvent(failedEvents, GatewayTextEventFailed) {
		t.Fatalf("failed generation events = %v", gatewayTextEventTypes(failedEvents))
	}
}

func TestProviderVideoCreateIdempotencyIntegration(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run provider request integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for provider request integration tests")
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
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID) })
	service := NewService(pool, vault)
	service.EnableGatewayRuntime()
	service.SetStorage(newMemoryObjectStorage())

	req := GatewayVideoCreateTaskRequest{
		OrganizationID: orgID, ProjectID: projectID, ProviderModelID: modelID, IdempotencyKey: "video-create-once",
		Input: mustJSON(map[string]any{"prompt": "create once", "duration": 5, "aspectRatio": "16:9", "resolution": "720p"}),
	}
	first, err := service.CreateVideoTask(ctx, req)
	if err != nil {
		t.Fatalf("first video create: %v", err)
	}
	second, err := service.CreateVideoTask(ctx, req)
	if err != nil {
		t.Fatalf("replay video create: %v", err)
	}
	if first.ProviderRequestID == "" || first.ProviderRequestID != second.ProviderRequestID || first.ProviderCallID != second.ProviderCallID || first.ProviderAsyncTaskID != second.ProviderAsyncTaskID {
		t.Fatalf("video replay mismatch: first=%+v second=%+v", first, second)
	}
	var calls, tasks int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM provider_call_logs WHERE provider_request_id = $1`, first.ProviderRequestID).Scan(&calls); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM provider_async_tasks WHERE provider_request_id = $1`, first.ProviderRequestID).Scan(&tasks); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || tasks != 1 {
		t.Fatalf("video create rows = calls %d tasks %d, want 1/1", calls, tasks)
	}
}

func providerTestPrompt(body map[string]any) string {
	messages, _ := body["messages"].([]any)
	if len(messages) == 0 {
		return ""
	}
	message, _ := messages[0].(map[string]any)
	prompt, _ := message["content"].(string)
	return prompt
}

func assertProviderRequestCounts(t *testing.T, ctx context.Context, db callWriter, providerRequestID string, wantCalls, wantCosts int) {
	t.Helper()
	var calls, costs int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM provider_call_logs WHERE provider_request_id = $1`, providerRequestID).Scan(&calls); err != nil {
		t.Fatalf("count provider calls: %v", err)
	}
	if err := db.QueryRow(ctx, `
		SELECT count(*)
		FROM cost_records cr
		JOIN provider_call_logs pcl ON pcl.id = cr.provider_call_id
		WHERE pcl.provider_request_id = $1
	`, providerRequestID).Scan(&costs); err != nil {
		t.Fatalf("count cost records: %v", err)
	}
	if calls != wantCalls || costs != wantCosts {
		t.Fatalf("provider request counts = calls %d costs %d, want %d/%d", calls, costs, wantCalls, wantCosts)
	}
}
