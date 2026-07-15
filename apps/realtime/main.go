package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	"github.com/Einzieg/cineweave/internal/config"
	"github.com/Einzieg/cineweave/internal/db"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/observability"
	"github.com/Einzieg/cineweave/internal/service"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultEventPageSize = 200
	eventStreamVersion   = "project-events.v2"
)

type bearerParser interface {
	ParseBearer(string) (auth.Principal, error)
}

type projectAuthorizer interface {
	Authorize(context.Context, auth.Principal, string, authz.Resource) error
}

type eventRepository interface {
	ProjectOrganization(context.Context, string) (string, error)
	Bounds(context.Context, string) (streamBounds, error)
	EventsAfter(context.Context, string, int64, int) ([]projectEvent, error)
}

type streamBounds struct {
	HighWatermark int64
	RetainedFrom  int64
}

type projectEvent struct {
	Position          int64
	EventID           string
	EventType         string
	SchemaVersion     int
	AggregateType     string
	AggregateID       string
	AggregateRevision *int64
	Payload           json.RawMessage
	CreatedAt         time.Time
}

type realtimeHandler struct {
	auth         bearerParser
	authorizer   projectAuthorizer
	events       eventRepository
	pollInterval time.Duration
	heartbeat    time.Duration
	pageSize     int
}

type pgEventRepository struct {
	pool *pgxpool.Pool
}

func main() {
	cfg := config.ServerFromEnv("realtime", "CINEWEAVE_REALTIME_ADDR", ":8081")
	logger := observability.Logger(cfg.Name, cfg.Env)
	ctx := context.Background()

	pool, err := db.Open(ctx, config.Get("DATABASE_URL", "postgres://cineweave:cineweave_dev_password@localhost:5432/cineweave?sslmode=disable"))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	jwtSecret := config.Get("CINEWEAVE_JWT_SECRET", config.DefaultJWTSecret)
	if err := config.ValidateProductionSecret(cfg.Env, "CINEWEAVE_JWT_SECRET", jwtSecret, config.DefaultJWTSecret); err != nil {
		log.Fatal(err)
	}
	authService := auth.NewService(
		pool,
		jwtSecret,
		config.Duration("CINEWEAVE_ACCESS_TOKEN_TTL", 2*time.Hour),
		config.Duration("CINEWEAVE_REFRESH_TOKEN_TTL", 30*24*time.Hour),
	)
	handler := &realtimeHandler{
		auth: authService, authorizer: authz.New(pool), events: &pgEventRepository{pool: pool},
		pollInterval: time.Second, heartbeat: 15 * time.Second, pageSize: defaultEventPageSize,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", httpx.HealthHandler("realtime"))
	mux.HandleFunc("/readyz", httpx.ReadyHandler("realtime", map[string]httpx.ReadinessCheck{
		"database": func(checkCtx context.Context) error {
			pingCtx, cancel := context.WithTimeout(checkCtx, 2*time.Second)
			defer cancel()
			return pool.Ping(pingCtx)
		},
	}))
	mux.Handle("/api/realtime/events", handler)

	if err := service.Serve(ctx, cfg, httpx.WithCORS(httpx.WithRequestID(mux)), logger); err != nil {
		log.Fatal(err)
	}
}

func (h *realtimeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpx.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method is not allowed", nil, false)
		return
	}
	if h == nil || h.auth == nil || h.authorizer == nil || h.events == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "REALTIME_UNAVAILABLE", "realtime service is unavailable", nil, true)
		return
	}

	principal, err := h.auth.ParseBearer(r.Header.Get("Authorization"))
	if err != nil {
		httpx.WriteError(w, r, http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "authentication is required", nil, false)
		return
	}
	projectID := strings.TrimSpace(r.URL.Query().Get("projectId"))
	if uuid.Validate(projectID) != nil {
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_FAILED", "projectId must be a valid UUID", nil, false)
		return
	}
	projectOrganizationID, err := h.events.ProjectOrganization(r.Context(), projectID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpx.WriteError(w, r, http.StatusNotFound, "PROJECT_NOT_FOUND", "project was not found", nil, false)
			return
		}
		httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error", nil, true)
		return
	}
	if strings.TrimSpace(principal.OrganizationID) == "" || principal.OrganizationID != projectOrganizationID {
		httpx.WriteError(w, r, http.StatusNotFound, "PROJECT_NOT_FOUND", "project was not found", nil, false)
		return
	}
	if err := h.authorizer.Authorize(r.Context(), principal, authz.PermissionProjectRead, authz.Resource{ProjectID: projectID}); err != nil {
		if errors.Is(err, authz.ErrAccessDenied) {
			httpx.WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "permission is required", nil, false)
			return
		}
		httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error", nil, true)
		return
	}

	cursor, supplied, err := requestCursor(r)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_EVENT_CURSOR", err.Error(), nil, false)
		return
	}
	bounds, err := h.events.Bounds(r.Context(), projectID)
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error", nil, true)
		return
	}
	if supplied && cursor < bounds.RetainedFrom-1 {
		w.Header().Set("X-CineWeave-Stream-High-Watermark", strconv.FormatInt(bounds.HighWatermark, 10))
		httpx.WriteError(w, r, http.StatusGone, "EVENT_CURSOR_EXPIRED", "event cursor is outside the retention window", map[string]any{
			"highWatermark": bounds.HighWatermark, "retainedFrom": bounds.RetainedFrom,
		}, false)
		return
	}
	if supplied && cursor > bounds.HighWatermark {
		httpx.WriteError(w, r, http.StatusConflict, "EVENT_CURSOR_AHEAD", "event cursor is ahead of the project stream", map[string]any{
			"highWatermark": bounds.HighWatermark,
		}, false)
		return
	}
	if !supplied {
		cursor = bounds.HighWatermark
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		httpx.WriteError(w, r, http.StatusInternalServerError, "STREAM_UNSUPPORTED", "streaming is not supported", nil, false)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("X-CineWeave-Stream-High-Watermark", strconv.FormatInt(bounds.HighWatermark, 10))
	fmt.Fprint(w, "retry: 2000\n")
	_ = writeSSEEvent(w, "stream.ready", 0, mustEventJSON(map[string]any{
		"status": "connected", "streamVersion": eventStreamVersion,
		"cursor": cursor, "highWatermark": bounds.HighWatermark, "retainedFrom": bounds.RetainedFrom,
		"createdAt": time.Now().UTC().Format(time.RFC3339Nano),
	}))
	flusher.Flush()

	h.streamProjectEvents(r.Context(), w, flusher, projectID, cursor)
}

func requestCursor(r *http.Request) (int64, bool, error) {
	raw := strings.TrimSpace(r.Header.Get("Last-Event-ID"))
	if raw == "" {
		raw = strings.TrimSpace(r.URL.Query().Get("cursor"))
	}
	if raw == "" {
		return 0, false, nil
	}
	cursor, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || cursor < 0 {
		return 0, false, fmt.Errorf("event cursor must be a non-negative integer")
	}
	return cursor, true, nil
}

func (h *realtimeHandler) streamProjectEvents(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, projectID string, cursor int64) {
	pollInterval := h.pollInterval
	if pollInterval <= 0 {
		pollInterval = time.Second
	}
	heartbeatInterval := h.heartbeat
	if heartbeatInterval <= 0 {
		heartbeatInterval = 15 * time.Second
	}
	pageSize := h.pageSize
	if pageSize <= 0 {
		pageSize = defaultEventPageSize
	}
	poll := time.NewTicker(pollInterval)
	defer poll.Stop()
	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()

	for {
		next, written, err := drainProjectEvents(ctx, w, h.events, projectID, cursor, pageSize)
		if err != nil {
			_ = writeSSEEvent(w, "stream.error", 0, mustEventJSON(map[string]any{
				"code": "EVENT_STREAM_READ_FAILED", "retryable": true,
			}))
			flusher.Flush()
			return
		}
		cursor = next
		if written > 0 {
			flusher.Flush()
		}
		select {
		case <-ctx.Done():
			return
		case <-poll.C:
		case <-heartbeat.C:
			fmt.Fprintf(w, ": keepalive %s cursor=%d\n\n", time.Now().UTC().Format(time.RFC3339Nano), cursor)
			flusher.Flush()
		}
	}
}

func drainProjectEvents(ctx context.Context, w http.ResponseWriter, repository eventRepository, projectID string, cursor int64, pageSize int) (int64, int, error) {
	if pageSize <= 0 {
		pageSize = defaultEventPageSize
	}
	written := 0
	for {
		events, err := repository.EventsAfter(ctx, projectID, cursor, pageSize)
		if err != nil {
			return cursor, written, err
		}
		for _, event := range events {
			if event.Position <= cursor {
				return cursor, written, fmt.Errorf("project event positions are not strictly increasing")
			}
			payload := enrichEventPayload(event)
			if err := writeSSEEvent(w, event.EventType, event.Position, payload); err != nil {
				return cursor, written, err
			}
			cursor = event.Position
			written++
		}
		if len(events) < pageSize {
			return cursor, written, nil
		}
	}
}

func writeSSEEvent(w http.ResponseWriter, eventType string, position int64, payload json.RawMessage) error {
	eventType = strings.NewReplacer("\r", "", "\n", "").Replace(strings.TrimSpace(eventType))
	if eventType == "" {
		return fmt.Errorf("event type is required")
	}
	if position > 0 {
		if _, err := fmt.Fprintf(w, "id: %d\n", position); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "event: %s\n", eventType); err != nil {
		return err
	}
	compact := &bytes.Buffer{}
	if err := json.Compact(compact, payload); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "data: %s\n\n", compact.String())
	return err
}

func enrichEventPayload(event projectEvent) json.RawMessage {
	body := map[string]any{}
	if len(event.Payload) > 0 {
		if err := json.Unmarshal(event.Payload, &body); err != nil {
			return event.Payload
		}
	}
	schemaVersion := event.SchemaVersion
	if schemaVersion <= 0 {
		schemaVersion = 1
	}
	body["eventId"] = event.EventID
	body["schemaVersion"] = schemaVersion
	body["streamPosition"] = event.Position
	body["aggregateId"] = event.AggregateID
	body["aggregateType"] = event.AggregateType
	body["createdAt"] = event.CreatedAt.UTC().Format(time.RFC3339Nano)
	if event.AggregateRevision != nil {
		body["aggregateRevision"] = *event.AggregateRevision
	}
	switch event.AggregateType {
	case "workflow_run":
		if _, ok := body["workflowRunId"]; !ok {
			body["workflowRunId"] = event.AggregateID
		}
	case "workflow_node_run":
		if _, ok := body["nodeRunId"]; !ok {
			body["nodeRunId"] = event.AggregateID
		}
	case "script_episode":
		if _, ok := body["scriptEpisodeId"]; !ok {
			body["scriptEpisodeId"] = event.AggregateID
		}
	case "storyboard_shot":
		if _, ok := body["shotId"]; !ok {
			body["shotId"] = event.AggregateID
		}
	case "canonical_asset":
		if _, ok := body["assetId"]; !ok {
			body["assetId"] = event.AggregateID
		}
	}
	return mustEventJSON(body)
}

func mustEventJSON(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

func (r *pgEventRepository) ProjectOrganization(ctx context.Context, projectID string) (string, error) {
	var organizationID string
	err := r.pool.QueryRow(ctx, `SELECT organization_id::text FROM projects WHERE id = $1`, projectID).Scan(&organizationID)
	return organizationID, err
}

func (r *pgEventRepository) Bounds(ctx context.Context, projectID string) (streamBounds, error) {
	var bounds streamBounds
	err := r.pool.QueryRow(ctx, `
		SELECT
			COALESCE(stream.next_position - 1, 0),
			COALESCE(
				(SELECT min(event.stream_position)
				 FROM project_event_log event
				 WHERE event.project_id = project.id AND event.expires_at > now()),
				COALESCE(stream.next_position, 1)
			)
		FROM projects project
		LEFT JOIN project_event_streams stream ON stream.project_id = project.id
		WHERE project.id = $1
	`, projectID).Scan(&bounds.HighWatermark, &bounds.RetainedFrom)
	return bounds, err
}

func (r *pgEventRepository) EventsAfter(ctx context.Context, projectID string, cursor int64, limit int) ([]projectEvent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT stream_position, event_id::text, event_type, schema_version, aggregate_type,
		       COALESCE(aggregate_id::text, ''), aggregate_revision, payload, created_at
		FROM project_event_log
		WHERE project_id = $1
		  AND stream_position > $2
		  AND expires_at > now()
		ORDER BY stream_position
		LIMIT $3
	`, projectID, cursor, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]projectEvent, 0, limit)
	for rows.Next() {
		var item projectEvent
		var aggregateRevision sql.NullInt64
		if err := rows.Scan(
			&item.Position, &item.EventID, &item.EventType, &item.SchemaVersion, &item.AggregateType,
			&item.AggregateID, &aggregateRevision, &item.Payload, &item.CreatedAt,
		); err != nil {
			return nil, err
		}
		if aggregateRevision.Valid {
			item.AggregateRevision = &aggregateRevision.Int64
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
