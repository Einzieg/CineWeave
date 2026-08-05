package provider

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestGatewayRoutingIntegration(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run provider gateway routing integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for provider gateway routing integration tests")
	}

	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)

	vault, err := NewVault("")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}

	t.Run("text fallback", func(t *testing.T) {
		upstream := httptest.NewServer(textRoutingMock(t))
		defer upstream.Close()
		orgID, _, firstModelID := seedGatewayIntegrationData(t, ctx, pool, vault, upstream.URL)
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
		})
		accountID := providerAccountIDForModel(t, ctx, pool, firstModelID)
		secondModelID := insertProviderModelForRouting(t, ctx, pool, accountID, "gpt-routing-fallback", "text", `["text.generate"]`, `{"inputTokenPer1K":"0.0100","outputTokenPer1K":"0.0200"}`)
		profileKey := insertRoutingProfile(t, ctx, pool, orgID, "text_routing_default", []string{firstModelID, secondModelID})

		service := NewService(pool, vault)
		service.EnableGatewayRuntime()
		resp, err := service.GenerateText(ctx, GatewayTextRequest{
			OrganizationID:  orgID,
			ModelProfileKey: profileKey,
			Input:           mustJSON(map[string]any{"prompt": "route text"}),
		})
		if err != nil {
			t.Fatalf("GenerateText: %v", err)
		}
		if resp.Status != "succeeded" || resp.ModelID != secondModelID || resp.Output.Text != "fallback text ok" {
			t.Fatalf("text response = %+v", resp)
		}
		assertRoutingAttempts(t, resp.Attempts, []string{"failed", "succeeded"}, []string{firstModelID, secondModelID})
		assertProviderCallStatus(t, ctx, pool, resp.Attempts[0].ProviderCallID, "failed", CodeUpstreamInternalError)
		assertProviderCallStatus(t, ctx, pool, resp.Attempts[1].ProviderCallID, "succeeded", "")
	})

	t.Run("model default reasoning level reaches upstream", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var request map[string]any
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if request["reasoning_effort"] != "high" {
				t.Errorf("reasoning_effort = %#v, want high", request["reasoning_effort"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{"message": map[string]any{"content": "reasoned"}}},
			})
		}))
		defer upstream.Close()
		orgID, _, modelID := seedGatewayIntegrationData(t, ctx, pool, vault, upstream.URL)
		t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID) })
		if _, err := pool.Exec(ctx, `
			UPDATE provider_model_capabilities
			SET provider_options_schema = '{"xCapabilities":{"supportsReasoning":true,"supportsReasoningLevels":true,"reasoningLevels":["low","medium","high"],"defaultReasoningLevel":"high"}}'
			WHERE provider_model_id = $1
		`, modelID); err != nil {
			t.Fatalf("configure reasoning capability: %v", err)
		}
		profileKey := insertRoutingProfile(t, ctx, pool, orgID, "text_reasoning_default", []string{modelID})

		service := NewService(pool, vault)
		service.EnableGatewayRuntime()
		resp, err := service.GenerateText(ctx, GatewayTextRequest{
			OrganizationID:  orgID,
			ModelProfileKey: profileKey,
			Input:           mustJSON(map[string]any{"prompt": "reason"}),
		})
		if err != nil {
			t.Fatalf("GenerateText: %v", err)
		}
		if resp.Status != "succeeded" || resp.Output.Text != "reasoned" {
			t.Fatalf("text response = %+v", resp)
		}
		var requestSnapshot map[string]any
		if err := pool.QueryRow(ctx, `SELECT request_snapshot FROM provider_call_logs WHERE id = $1`, resp.ProviderCallID).Scan(&requestSnapshot); err != nil {
			t.Fatalf("load request snapshot: %v", err)
		}
		if requestSnapshot["reasoning_effort"] != "high" {
			t.Fatalf("recorded reasoning_effort = %#v, want high", requestSnapshot["reasoning_effort"])
		}
	})

	t.Run("stream fallback before first delta", func(t *testing.T) {
		var firstCalls atomic.Int64
		var secondCalls atomic.Int64
		upstream := httptest.NewServer(streamRoutingMock(t, &firstCalls, &secondCalls, false))
		defer upstream.Close()
		orgID, _, firstModelID := seedGatewayIntegrationData(t, ctx, pool, vault, upstream.URL)
		t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID) })
		accountID := providerAccountIDForModel(t, ctx, pool, firstModelID)
		secondModelID := insertProviderModelForRouting(t, ctx, pool, accountID, "gpt-stream-fallback", "text", `["text.stream"]`, `{}`)
		profileKey := insertRoutingProfile(t, ctx, pool, orgID, "text_stream_pre_delta", []string{firstModelID, secondModelID})

		service := NewService(pool, vault)
		service.EnableGatewayRuntime()
		var events []GatewayTextStreamEvent
		resp, err := service.StreamTextEvents(ctx, GatewayTextRequest{
			OrganizationID: orgID, ModelProfileKey: profileKey, IdempotencyKey: "stream-pre-delta",
			Input: mustJSON(map[string]any{"messages": []map[string]string{{"role": "user", "content": "stream"}}}),
		}, func(event GatewayTextStreamEvent) error {
			events = append(events, event)
			return nil
		})
		if err != nil {
			t.Fatalf("StreamTextEvents: %v", err)
		}
		if resp.Status != "succeeded" || resp.AttemptGeneration != 1 || resp.AttemptSequence != 2 || resp.ModelID != secondModelID || resp.Output.Text != "fallback stream ok" {
			t.Fatalf("stream response = %+v", resp)
		}
		if firstCalls.Load() != 1 || secondCalls.Load() != 1 {
			t.Fatalf("upstream calls first=%d second=%d, want 1/1", firstCalls.Load(), secondCalls.Load())
		}
		assertTextStreamEventIdentity(t, events, resp.ProviderRequestID, resp.ProviderCallID, 1, 2)
	})

	t.Run("stream never falls back after first delta", func(t *testing.T) {
		var firstCalls atomic.Int64
		var secondCalls atomic.Int64
		upstream := httptest.NewServer(streamRoutingMock(t, &firstCalls, &secondCalls, true))
		defer upstream.Close()
		orgID, _, firstModelID := seedGatewayIntegrationData(t, ctx, pool, vault, upstream.URL)
		t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID) })
		accountID := providerAccountIDForModel(t, ctx, pool, firstModelID)
		secondModelID := insertProviderModelForRouting(t, ctx, pool, accountID, "gpt-stream-forbidden-fallback", "text", `["text.stream"]`, `{}`)
		profileKey := insertRoutingProfile(t, ctx, pool, orgID, "text_stream_post_delta", []string{firstModelID, secondModelID})

		service := NewService(pool, vault)
		service.EnableGatewayRuntime()
		var events []GatewayTextStreamEvent
		resp, err := service.StreamTextEvents(ctx, GatewayTextRequest{
			OrganizationID: orgID, ModelProfileKey: profileKey, IdempotencyKey: "stream-post-delta",
			Input: mustJSON(map[string]any{"messages": []map[string]string{{"role": "user", "content": "stream"}}}),
		}, func(event GatewayTextStreamEvent) error {
			events = append(events, event)
			return nil
		})
		var standardErr *StandardErrorError
		if !errors.As(err, &standardErr) || standardErr.Standard.Code != CodeUpstreamStreamTruncated {
			t.Fatalf("StreamTextEvents error = %v, response=%+v", err, resp)
		}
		if firstCalls.Load() != 1 || secondCalls.Load() != 0 || len(resp.Attempts) != 1 {
			t.Fatalf("post-delta calls first=%d second=%d attempts=%d", firstCalls.Load(), secondCalls.Load(), len(resp.Attempts))
		}
		if !hasGatewayTextEvent(events, GatewayTextEventDelta) || !hasGatewayTextEvent(events, GatewayTextEventAttemptFailed) || !hasGatewayTextEvent(events, GatewayTextEventFailed) {
			t.Fatalf("post-delta events = %+v", gatewayTextEventTypes(events))
		}
	})

	t.Run("stream retries request timeout three times before succeeding", func(t *testing.T) {
		var calls atomic.Int64
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			call := calls.Add(1)
			if call <= gatewayTextMaxRetries {
				w.WriteHeader(http.StatusRequestTimeout)
				_, _ = w.Write([]byte(`{"error":{"code":"request_timeout","message":"request timed out"}}`))
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"retry succeeded\"}}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		}))
		defer upstream.Close()
		orgID, _, modelID := seedGatewayIntegrationData(t, ctx, pool, vault, upstream.URL)
		t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID) })

		service := NewService(pool, vault)
		service.EnableGatewayRuntime()
		resp, err := service.StreamTextEvents(ctx, GatewayTextRequest{
			OrganizationID:  orgID,
			ProviderModelID: modelID,
			IdempotencyKey:  "stream-timeout-retry",
			Input:           mustJSON(map[string]any{"messages": []map[string]string{{"role": "user", "content": "retry"}}}),
		}, nil)
		if err != nil {
			t.Fatalf("StreamTextEvents: %v", err)
		}
		if resp.Status != "succeeded" || resp.Output.Text != "retry succeeded" {
			t.Fatalf("stream response = %+v", resp)
		}
		if calls.Load() != gatewayTextMaxAttemptsPerSelection || len(resp.Attempts) != gatewayTextMaxAttemptsPerSelection {
			t.Fatalf("upstream calls=%d attempts=%d, want %d", calls.Load(), len(resp.Attempts), gatewayTextMaxAttemptsPerSelection)
		}
		for i, attempt := range resp.Attempts {
			wantStatus := "failed"
			wantCode := CodeUpstreamTimeout
			if i == gatewayTextMaxAttemptsPerSelection-1 {
				wantStatus = "succeeded"
				wantCode = ""
			}
			if attempt.ProviderModelID != modelID || attempt.Status != wantStatus {
				t.Fatalf("attempt %d = %+v, want model=%s status=%s", i+1, attempt, modelID, wantStatus)
			}
			assertProviderCallStatus(t, ctx, pool, attempt.ProviderCallID, wantStatus, wantCode)
		}
	})

	t.Run("cancelled retry wait preserves failed request result", func(t *testing.T) {
		var calls atomic.Int64
		firstResponse := make(chan struct{}, 1)
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			w.WriteHeader(http.StatusRequestTimeout)
			_, _ = w.Write([]byte(`{"error":{"code":"request_timeout","message":"request timed out"}}`))
			select {
			case firstResponse <- struct{}{}:
			default:
			}
		}))
		defer upstream.Close()
		orgID, _, modelID := seedGatewayIntegrationData(t, ctx, pool, vault, upstream.URL)
		t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID) })

		requestCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		go func() {
			<-firstResponse
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		service := NewService(pool, vault)
		service.EnableGatewayRuntime()
		resp, err := service.GenerateText(requestCtx, GatewayTextRequest{
			OrganizationID:  orgID,
			ProviderModelID: modelID,
			IdempotencyKey:  "cancelled-retry-wait",
			Input:           mustJSON(map[string]any{"prompt": "retry"}),
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("GenerateText error = %v, want context.Canceled", err)
		}
		if resp.Status != "failed" || resp.Error == nil || resp.Error.Code != CodeUpstreamTimeout {
			t.Fatalf("response = %+v, want failed timeout", resp)
		}
		if calls.Load() != 1 || len(resp.Attempts) != 1 {
			t.Fatalf("upstream calls=%d attempts=%d, want 1/1", calls.Load(), len(resp.Attempts))
		}

		var requestStatus, errorCode string
		if err := pool.QueryRow(context.Background(), `
			SELECT status, error_code
			FROM provider_requests
			WHERE id = $1
		`, resp.ProviderRequestID).Scan(&requestStatus, &errorCode); err != nil {
			t.Fatalf("load provider request: %v", err)
		}
		if requestStatus != "failed" || errorCode != CodeUpstreamTimeout {
			t.Fatalf("provider request status=%q error=%q, want failed/%s", requestStatus, errorCode, CodeUpstreamTimeout)
		}
	})

	t.Run("image guard fallback", func(t *testing.T) {
		upstream := httptest.NewServer(imageRoutingMock(t))
		defer upstream.Close()
		orgID, _, projectID, firstModelID := seedGatewayImageIntegrationData(t, ctx, pool, vault, upstream.URL)
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
		})
		accountID := providerAccountIDForModel(t, ctx, pool, firstModelID)
		secondModelID := insertProviderModelForRouting(t, ctx, pool, accountID, "gpt-image-routing-fallback", "image", `["image.generate"]`, `{"imageCost":"0.0050"}`)
		profileKey := insertRoutingProfile(t, ctx, pool, orgID, "image_routing_default", []string{firstModelID, secondModelID})
		insertLimitPolicy(t, ctx, pool, orgID, accountID, firstModelID, TaskTypeImageGenerate, map[string]any{"max_concurrency": 0})

		service := NewService(pool, vault)
		service.EnableGatewayRuntime()
		service.SetStorage(newMemoryObjectStorage())
		resp, err := service.GenerateImage(ctx, GatewayImageRequest{
			OrganizationID:  orgID,
			ProjectID:       projectID,
			ModelProfileKey: profileKey,
			Input:           mustJSON(map[string]any{"prompt": "route image", "size": "1024x1024"}),
		})
		if err != nil {
			t.Fatalf("GenerateImage: %v", err)
		}
		if resp.Status != "succeeded" || resp.ModelID != secondModelID || resp.Output.ArtifactID == "" {
			t.Fatalf("image response = %+v", resp)
		}
		assertRoutingAttempts(t, resp.Attempts, []string{"blocked", "succeeded"}, []string{firstModelID, secondModelID})
		assertProviderCallStatus(t, ctx, pool, resp.Attempts[0].ProviderCallID, "blocked", CodeProviderConcurrencyLimited)
		assertNoCostRecord(t, ctx, pool, resp.Attempts[0].ProviderCallID)
	})

	t.Run("video create fallback", func(t *testing.T) {
		upstream := httptest.NewServer(videoCreateRoutingMock(t))
		defer upstream.Close()
		orgID, _, projectID, firstModelID := seedGatewayVideoIntegrationData(t, ctx, pool, vault, upstream.URL)
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
		})
		accountID := providerAccountIDForModel(t, ctx, pool, firstModelID)
		secondModelID := insertProviderModelForRouting(t, ctx, pool, accountID, "video-routing-fallback", "video", `["video.generate"]`, `{"videoCostPerSecond":"0.0500"}`)
		profileKey := insertRoutingProfile(t, ctx, pool, orgID, "video_routing_default", []string{firstModelID, secondModelID})

		service := NewService(pool, vault)
		service.EnableGatewayRuntime()
		resp, err := service.CreateVideoTask(ctx, GatewayVideoCreateTaskRequest{
			OrganizationID:  orgID,
			ProjectID:       projectID,
			ModelProfileKey: profileKey,
			Input: mustJSON(map[string]any{
				"prompt":      "route video",
				"duration":    5,
				"aspectRatio": "16:9",
				"resolution":  "720p",
			}),
		})
		if err != nil {
			t.Fatalf("CreateVideoTask: %v", err)
		}
		if resp.Status != "running" || resp.ModelID != secondModelID || resp.ProviderAsyncTaskID == "" {
			t.Fatalf("video response = %+v", resp)
		}
		assertRoutingAttempts(t, resp.Attempts, []string{"failed", "running"}, []string{firstModelID, secondModelID})
		assertAsyncTaskModel(t, ctx, pool, resp.ProviderAsyncTaskID, secondModelID)
		assertProviderCallStatus(t, ctx, pool, resp.Attempts[0].ProviderCallID, "failed", CodeUpstreamInternalError)
	})
}

func textRoutingMock(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode text request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if request["model"] == "gpt-integration" {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":{"code":"server_error"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"fallback text ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`))
	})
}

func streamRoutingMock(t *testing.T, firstCalls, secondCalls *atomic.Int64, truncateAfterDelta bool) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		model, _ := request["model"].(string)
		if model == "gpt-integration" {
			firstCalls.Add(1)
			if !truncateAfterDelta {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(`{"error":{"code":"server_error"}}`))
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"))
			return
		}
		secondCalls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"fallback stream ok\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	})
}

func assertTextStreamEventIdentity(t *testing.T, events []GatewayTextStreamEvent, requestID, acceptedCallID string, generation, attemptSequence int) {
	t.Helper()
	var delta *GatewayTextDelta
	for _, event := range events {
		if event.Type == GatewayTextEventDelta {
			delta = event.Delta
			break
		}
	}
	if delta == nil || delta.SchemaVersion != 2 || delta.ProviderRequestID != requestID || delta.ProviderCallID != acceptedCallID || delta.AttemptGeneration != generation || delta.AttemptSequence != attemptSequence || delta.Sequence != 1 {
		t.Fatalf("accepted delta identity = %+v events=%v", delta, gatewayTextEventTypes(events))
	}
	if !hasGatewayTextEvent(events, GatewayTextEventAttemptFailed) || !hasGatewayTextEvent(events, GatewayTextEventCompleted) {
		t.Fatalf("stream events = %v", gatewayTextEventTypes(events))
	}
}

func hasGatewayTextEvent(events []GatewayTextStreamEvent, eventType string) bool {
	for _, event := range events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}

func gatewayTextEventTypes(events []GatewayTextStreamEvent) []string {
	types := make([]string, 0, len(events))
	for _, event := range events {
		types = append(types, event.Type)
	}
	return types
}

func imageRoutingMock(t *testing.T) http.Handler {
	t.Helper()
	pngBody := testPNGBytes(t)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || (r.URL.Path != "/images/generations" && r.URL.Path != "/v1/images/generations") {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&map[string]any{})
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{
				"b64_json":  base64.StdEncoding.EncodeToString(pngBody),
				"mime_type": "image/png",
			}},
		})
	})
}

func videoCreateRoutingMock(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/video/create" {
			http.NotFound(w, r)
			return
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode video request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if request["model"] == "video-integration-model" {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":{"code":"server_error"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"taskId": "routing-task", "status": "processing"})
	})
}

func insertRoutingProfile(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, key string, modelIDs []string) string {
	t.Helper()
	var profileID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO model_profiles(organization_id, profile_key, name, purpose, routing_strategy, fallback_strategy)
		VALUES ($1, $2, $3, $4, 'priority_with_fallback', '{"enabled":true,"maxAttempts":3}')
		RETURNING id
	`, orgID, key, key, key).Scan(&profileID); err != nil {
		t.Fatalf("insert routing profile: %v", err)
	}
	for i, modelID := range modelIDs {
		if _, err := pool.Exec(ctx, `
			INSERT INTO model_profile_bindings(model_profile_id, provider_model_id, priority, weight, enabled)
			VALUES ($1, $2, $3, 100, true)
		`, profileID, modelID, i+1); err != nil {
			t.Fatalf("insert routing binding: %v", err)
		}
	}
	return key
}

func insertProviderModelForRouting(t *testing.T, ctx context.Context, pool *pgxpool.Pool, accountID, modelKey, modality, taskTypes, pricing string) string {
	t.Helper()
	var modelID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO provider_models(provider_account_id, model_key, display_name, modality, status)
		VALUES ($1, $2, $3, $4, 'active')
		RETURNING id
	`, accountID, modelKey, "Routing "+modelKey, modality).Scan(&modelID); err != nil {
		t.Fatalf("insert routing model: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO provider_model_capabilities(
			provider_model_id, task_types, input_limits, output_limits, quality_tiers, provider_options_schema, pricing_policy
		)
		VALUES ($1, $2, '{}', '{}', '[]', '{}', $3)
	`, modelID, taskTypes, pricing); err != nil {
		t.Fatalf("insert routing capability: %v", err)
	}
	return modelID
}

func assertRoutingAttempts(t *testing.T, attempts []GatewayAttempt, statuses, modelIDs []string) {
	t.Helper()
	if len(attempts) != len(statuses) {
		t.Fatalf("attempt count = %d, want %d: %+v", len(attempts), len(statuses), attempts)
	}
	for i := range statuses {
		if attempts[i].Status != statuses[i] || attempts[i].ProviderModelID != modelIDs[i] {
			t.Fatalf("attempt %d = status=%s model=%s, want %s/%s", i, attempts[i].Status, attempts[i].ProviderModelID, statuses[i], modelIDs[i])
		}
	}
}

func assertProviderCallStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, providerCallID, wantStatus, wantCode string) {
	t.Helper()
	var status string
	var errorCode sql.NullString
	if err := pool.QueryRow(ctx, `
		SELECT status, error_code
		FROM provider_call_logs
		WHERE id = $1
	`, providerCallID).Scan(&status, &errorCode); err != nil {
		t.Fatalf("select provider call status: %v", err)
	}
	if status != wantStatus || errorCode.String != wantCode {
		t.Fatalf("provider call = status=%s code=%s, want %s/%s", status, errorCode.String, wantStatus, wantCode)
	}
}

func assertAsyncTaskModel(t *testing.T, ctx context.Context, pool *pgxpool.Pool, taskID, modelID string) {
	t.Helper()
	var got string
	if err := pool.QueryRow(ctx, `SELECT provider_model_id::text FROM provider_async_tasks WHERE id = $1`, taskID).Scan(&got); err != nil {
		t.Fatalf("select async task model: %v", err)
	}
	if got != modelID {
		t.Fatalf("async task model = %s, want %s", got, modelID)
	}
}
