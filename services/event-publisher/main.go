package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Einzieg/cineweave/internal/config"
	"github.com/Einzieg/cineweave/internal/db"
	"github.com/Einzieg/cineweave/internal/observability"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
)

const (
	streamName    = "CINEWEAVE_EVENTS"
	subjectPrefix = "cineweave.events"
)

type outboxEvent struct {
	ID             string          `json:"id"`
	OrganizationID string          `json:"organizationId"`
	ProjectID      *string         `json:"projectId,omitempty"`
	EventType      string          `json:"eventType"`
	AggregateType  string          `json:"aggregateType"`
	AggregateID    *string         `json:"aggregateId,omitempty"`
	Payload        json.RawMessage `json:"payload"`
	CreatedAt      time.Time       `json:"createdAt"`
}

func main() {
	logger := observability.Logger("event-publisher", config.Get("CINEWEAVE_ENV", "development"))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(ctx, config.Get("DATABASE_URL", "postgres://cineweave:cineweave_dev_password@localhost:5432/cineweave?sslmode=disable"))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	nc, err := nats.Connect(config.Get("NATS_URL", "nats://localhost:4222"))
	if err != nil {
		log.Fatal(err)
	}
	defer nc.Drain()

	js, err := nc.JetStream()
	if err != nil {
		log.Fatal(err)
	}
	if err := ensureStream(js); err != nil {
		log.Fatal(err)
	}

	publisher := publisher{db: pool, js: js, logger: logger}
	ticker := time.NewTicker(config.Duration("CINEWEAVE_EVENT_PUBLISH_INTERVAL", time.Second))
	defer ticker.Stop()
	batchSize := config.Int("CINEWEAVE_EVENT_PUBLISH_BATCH", 50)

	for {
		published, err := publisher.publishBatch(ctx, batchSize)
		if err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("event publish batch failed", "error", err)
		}
		if published > 0 {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

type publisher struct {
	db     *pgxpool.Pool
	js     nats.JetStreamContext
	logger *slog.Logger
}

func ensureStream(js nats.JetStreamContext) error {
	cfg := &nats.StreamConfig{
		Name:     streamName,
		Subjects: []string{subjectPrefix + ".>"},
		Storage:  nats.FileStorage,
	}
	if _, err := js.AddStream(cfg); err != nil {
		if strings.Contains(err.Error(), "stream name already in use") {
			_, err = js.UpdateStream(cfg)
		}
		return err
	}
	return nil
}

func (p publisher) publishBatch(ctx context.Context, limit int) (int, error) {
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		UPDATE event_outbox
		SET status = 'publishing', attempts = attempts + 1
		WHERE id IN (
			SELECT id
			FROM event_outbox
			WHERE status IN ('pending', 'failed') AND next_attempt_at <= now()
			ORDER BY created_at ASC
			FOR UPDATE SKIP LOCKED
			LIMIT $1
		)
		RETURNING id, organization_id, project_id, event_type, aggregate_type, aggregate_id, payload, created_at
	`, limit)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	events := make([]outboxEvent, 0)
	for rows.Next() {
		var event outboxEvent
		if err := rows.Scan(&event.ID, &event.OrganizationID, &event.ProjectID, &event.EventType, &event.AggregateType, &event.AggregateID, &event.Payload, &event.CreatedAt); err != nil {
			return 0, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}

	published := 0
	for _, event := range events {
		if err := p.publishEvent(ctx, event); err != nil {
			p.logger.Error("event publish failed", "eventId", event.ID, "eventType", event.EventType, "error", err)
			_ = p.markFailed(ctx, event.ID)
			continue
		}
		if err := p.markPublished(ctx, event.ID); err != nil {
			return published, err
		}
		published++
	}
	return published, nil
}

func (p publisher) publishEvent(ctx context.Context, event outboxEvent) error {
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	subject := subjectPrefix + "." + sanitizeSubjectToken(event.EventType)
	_, err = p.js.Publish(subject, raw, nats.Context(ctx), nats.MsgId(event.ID))
	return err
}

func (p publisher) markPublished(ctx context.Context, eventID string) error {
	_, err := p.db.Exec(ctx, `
		UPDATE event_outbox
		SET status = 'published', published_at = now()
		WHERE id = $1
	`, eventID)
	return err
}

func (p publisher) markFailed(ctx context.Context, eventID string) error {
	_, err := p.db.Exec(ctx, `
		UPDATE event_outbox
		SET status = 'failed', next_attempt_at = now() + (attempts * interval '5 seconds')
		WHERE id = $1
	`, eventID)
	return err
}

func sanitizeSubjectToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, ".", "_")
	value = strings.ReplaceAll(value, " ", "_")
	if value == "" {
		return "unknown"
	}
	return value
}
