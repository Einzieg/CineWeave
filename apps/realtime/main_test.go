package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/jackc/pgx/v5"
)

const realtimeTestProjectID = "11111111-1111-4111-8111-111111111111"

type fakeBearerParser struct {
	principal auth.Principal
	err       error
}

func (f fakeBearerParser) ParseBearer(string) (auth.Principal, error) {
	return f.principal, f.err
}

type fakeProjectAuthorizer struct {
	err    error
	called int
}

func (f *fakeProjectAuthorizer) Authorize(_ context.Context, _ auth.Principal, permission string, resource authz.Resource) error {
	f.called++
	if permission != authz.PermissionProjectRead || resource.ProjectID != realtimeTestProjectID {
		return fmt.Errorf("unexpected authorization request")
	}
	return f.err
}

type fakeEventRepository struct {
	organizationID string
	projectErr     error
	bounds         streamBounds
	events         []projectEvent
	pageCalls      int
}

func (f *fakeEventRepository) ProjectOrganization(context.Context, string) (string, error) {
	return f.organizationID, f.projectErr
}

func (f *fakeEventRepository) Bounds(context.Context, string) (streamBounds, error) {
	return f.bounds, nil
}

func (f *fakeEventRepository) EventsAfter(_ context.Context, _ string, cursor int64, limit int) ([]projectEvent, error) {
	f.pageCalls++
	items := make([]projectEvent, 0, limit)
	for _, event := range f.events {
		if event.Position > cursor {
			items = append(items, event)
			if len(items) == limit {
				break
			}
		}
	}
	return items, nil
}

func TestRealtimeRejectsUnauthenticatedSubscription(t *testing.T) {
	repository := &fakeEventRepository{}
	authorizer := &fakeProjectAuthorizer{}
	handler := &realtimeHandler{
		auth: fakeBearerParser{err: auth.ErrUnauthorized}, authorizer: authorizer, events: repository,
	}
	request := httptest.NewRequest(http.MethodGet, "/api/realtime/events?projectId="+realtimeTestProjectID, nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), "AUTHENTICATION_REQUIRED") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if authorizer.called != 0 {
		t.Fatalf("authorizer called %d times for unauthenticated request", authorizer.called)
	}
}

func TestRealtimeHidesCrossTenantProject(t *testing.T) {
	repository := &fakeEventRepository{organizationID: "organization-b"}
	authorizer := &fakeProjectAuthorizer{}
	handler := &realtimeHandler{
		auth:       fakeBearerParser{principal: auth.Principal{UserID: "user-a", OrganizationID: "organization-a"}},
		authorizer: authorizer, events: repository,
	}
	request := httptest.NewRequest(http.MethodGet, "/api/realtime/events?projectId="+realtimeTestProjectID, nil)
	request.Header.Set("Authorization", "Bearer token")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), "PROJECT_NOT_FOUND") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if authorizer.called != 0 {
		t.Fatalf("authorizer called %d times for cross-tenant request", authorizer.called)
	}
}

func TestRealtimeRejectsMissingProjectPermission(t *testing.T) {
	repository := &fakeEventRepository{organizationID: "organization-a"}
	authorizer := &fakeProjectAuthorizer{err: authz.ErrAccessDenied}
	handler := &realtimeHandler{
		auth:       fakeBearerParser{principal: auth.Principal{UserID: "user-a", OrganizationID: "organization-a"}},
		authorizer: authorizer, events: repository,
	}
	request := httptest.NewRequest(http.MethodGet, "/api/realtime/events?projectId="+realtimeTestProjectID, nil)
	request.Header.Set("Authorization", "Bearer token")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "FORBIDDEN") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestRealtimeReturnsGoneForExpiredCursor(t *testing.T) {
	repository := &fakeEventRepository{
		organizationID: "organization-a",
		bounds:         streamBounds{HighWatermark: 500, RetainedFrom: 301},
	}
	handler := &realtimeHandler{
		auth:       fakeBearerParser{principal: auth.Principal{UserID: "user-a", OrganizationID: "organization-a"}},
		authorizer: &fakeProjectAuthorizer{}, events: repository,
	}
	request := httptest.NewRequest(http.MethodGet, "/api/realtime/events?projectId="+realtimeTestProjectID, nil)
	request.Header.Set("Authorization", "Bearer token")
	request.Header.Set("Last-Event-ID", "299")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusGone || !strings.Contains(response.Body.String(), "EVENT_CURSOR_EXPIRED") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("X-CineWeave-Stream-High-Watermark"); got != "500" {
		t.Fatalf("high watermark header=%q", got)
	}
}

func TestDrainProjectEventsPaginatesBeyondTwoHundred(t *testing.T) {
	repository := &fakeEventRepository{organizationID: "organization-a"}
	for position := int64(1); position <= 500; position++ {
		repository.events = append(repository.events, projectEvent{
			Position: position, EventType: "workflow.node.progress", AggregateType: "workflow_node_run",
			AggregateID: fmt.Sprintf("node-%d", position), Payload: []byte(`{"progress":1}`),
		})
	}
	output := httptest.NewRecorder()

	cursor, written, err := drainProjectEvents(context.Background(), output, repository, realtimeTestProjectID, 0, 200)
	if err != nil {
		t.Fatalf("drain events: %v", err)
	}
	if cursor != 500 || written != 500 || repository.pageCalls != 3 {
		t.Fatalf("cursor=%d written=%d pageCalls=%d", cursor, written, repository.pageCalls)
	}
	if count := strings.Count(output.Body.String(), "event: workflow.node.progress"); count != 500 {
		t.Fatalf("serialized event count=%d", count)
	}
	if !strings.Contains(output.Body.String(), "id: 500\n") {
		t.Fatal("last stream position was not serialized")
	}
}

func TestEnrichProjectEventPayloadIncludesCatalogIdentity(t *testing.T) {
	revision := int64(7)
	event := projectEvent{
		Position: 42, EventID: "event-42", EventType: "script.episode.generated", SchemaVersion: 2,
		AggregateType: "script_episode", AggregateID: "episode-1", AggregateRevision: &revision,
		Payload: json.RawMessage(`{"scriptId":"script-1"}`), CreatedAt: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC),
	}
	var payload map[string]any
	if err := json.Unmarshal(enrichEventPayload(event), &payload); err != nil {
		t.Fatalf("decode enriched payload: %v", err)
	}
	if payload["eventId"] != "event-42" || payload["schemaVersion"] != float64(2) || payload["streamPosition"] != float64(42) {
		t.Fatalf("event identity = %#v", payload)
	}
	if payload["aggregateType"] != "script_episode" || payload["aggregateId"] != "episode-1" || payload["aggregateRevision"] != float64(7) || payload["scriptEpisodeId"] != "episode-1" {
		t.Fatalf("aggregate identity = %#v", payload)
	}
}

func TestRealtimeSetsProxySafeSSEHeaders(t *testing.T) {
	repository := &fakeEventRepository{
		organizationID: "organization-a",
		bounds:         streamBounds{HighWatermark: 12, RetainedFrom: 1},
	}
	handler := &realtimeHandler{
		auth:       fakeBearerParser{principal: auth.Principal{UserID: "user-a", OrganizationID: "organization-a"}},
		authorizer: &fakeProjectAuthorizer{}, events: repository,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodGet, "/api/realtime/events?projectId="+realtimeTestProjectID, nil).WithContext(ctx)
	request.Header.Set("Authorization", "Bearer token")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if got := response.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("content type = %q", got)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-cache, no-transform" {
		t.Fatalf("cache control = %q", got)
	}
	if got := response.Header().Get("X-Accel-Buffering"); got != "no" {
		t.Fatalf("X-Accel-Buffering = %q", got)
	}
	if got := response.Header().Get("X-CineWeave-Stream-High-Watermark"); got != "12" {
		t.Fatalf("high watermark = %q", got)
	}
	if !strings.Contains(response.Body.String(), "event: stream.ready") {
		t.Fatalf("stream ready event was not emitted: %s", response.Body.String())
	}
}

func TestRealtimeReturnsNotFoundForMissingProject(t *testing.T) {
	repository := &fakeEventRepository{projectErr: pgx.ErrNoRows}
	handler := &realtimeHandler{
		auth:       fakeBearerParser{principal: auth.Principal{UserID: "user-a", OrganizationID: "organization-a"}},
		authorizer: &fakeProjectAuthorizer{}, events: repository,
	}
	request := httptest.NewRequest(http.MethodGet, "/api/realtime/events?projectId="+realtimeTestProjectID, nil)
	request.Header.Set("Authorization", "Bearer token")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestRealtimePropagatesUnexpectedAuthorizationFailure(t *testing.T) {
	repository := &fakeEventRepository{organizationID: "organization-a"}
	handler := &realtimeHandler{
		auth:       fakeBearerParser{principal: auth.Principal{UserID: "user-a", OrganizationID: "organization-a"}},
		authorizer: &fakeProjectAuthorizer{err: errors.New("database unavailable")}, events: repository,
	}
	request := httptest.NewRequest(http.MethodGet, "/api/realtime/events?projectId="+realtimeTestProjectID, nil)
	request.Header.Set("Authorization", "Bearer token")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
