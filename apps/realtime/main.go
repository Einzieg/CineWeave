package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/Einzieg/cineweave/internal/config"
	"github.com/Einzieg/cineweave/internal/db"
	"github.com/Einzieg/cineweave/internal/httpx"
	"github.com/Einzieg/cineweave/internal/observability"
	"github.com/Einzieg/cineweave/internal/service"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	cfg := config.ServerFromEnv("realtime", "CINEWEAVE_REALTIME_ADDR", ":8081")
	logger := observability.Logger(cfg.Name, cfg.Env)
	ctx := context.Background()

	pool, err := db.Open(ctx, config.Get("DATABASE_URL", "postgres://cineweave:cineweave_dev_password@localhost:5432/cineweave?sslmode=disable"))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", httpx.HealthHandler("realtime"))
	mux.HandleFunc("/readyz", httpx.HealthHandler("realtime"))
	mux.HandleFunc("/api/realtime/events", events(pool))

	if err := service.Serve(ctx, cfg, httpx.WithCORS(httpx.WithRequestID(mux)), logger); err != nil {
		log.Fatal(err)
	}
}

func events(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpx.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method is not allowed", nil, false)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			httpx.WriteError(w, r, http.StatusInternalServerError, "STREAM_UNSUPPORTED", "streaming is not supported", nil, false)
			return
		}
		fmt.Fprintf(w, "event: queue.updated\n")
		fmt.Fprintf(w, "data: {\"status\":\"connected\",\"createdAt\":\"%s\"}\n\n", time.Now().UTC().Format(time.RFC3339))
		flusher.Flush()

		projectID := r.URL.Query().Get("projectId")
		if projectID != "" {
			streamProjectEvents(r.Context(), w, flusher, pool, projectID)
			return
		}
		waitForDisconnect(r.Context(), w, flusher)
	}
}

func streamProjectEvents(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, pool *pgxpool.Pool, projectID string) {
	seen := map[string]struct{}{}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		if writeProjectEvents(ctx, w, pool, projectID, seen) > 0 {
			flusher.Flush()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-heartbeat.C:
			fmt.Fprintf(w, ": keepalive %s\n\n", time.Now().UTC().Format(time.RFC3339))
			flusher.Flush()
		}
	}
}

func writeProjectEvents(ctx context.Context, w http.ResponseWriter, pool *pgxpool.Pool, projectID string, seen map[string]struct{}) int {
	rows, err := pool.Query(ctx, `
		SELECT id, event_type, payload
		FROM event_outbox
		WHERE project_id = $1
		ORDER BY created_at ASC, id ASC
		LIMIT 50
	`, projectID)
	if err != nil {
		return 0
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var id string
		var eventType string
		var payload json.RawMessage
		if err := rows.Scan(&id, &eventType, &payload); err != nil {
			return count
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		fmt.Fprintf(w, "id: %s\n", id)
		fmt.Fprintf(w, "event: %s\n", eventType)
		fmt.Fprintf(w, "data: %s\n\n", string(payload))
		count++
	}
	return count
}

func waitForDisconnect(ctx context.Context, w http.ResponseWriter, flusher http.Flusher) {
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			fmt.Fprintf(w, ": keepalive %s\n\n", time.Now().UTC().Format(time.RFC3339))
			flusher.Flush()
		}
	}
}
