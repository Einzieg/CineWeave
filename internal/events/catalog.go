package events

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

type Definition struct {
	Name                  string
	SchemaVersion         int
	ScopeType             string
	AggregateType         string
	RequiredPayloadFields []string
	Terminal              bool
	DeprecatedAt          string
}

type Execer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func DefinitionFor(name string) (Definition, bool) {
	definition, ok := Catalog[strings.TrimSpace(name)]
	return definition, ok
}

func Validate(name string, payload json.RawMessage) (Definition, error) {
	definition, ok := DefinitionFor(name)
	if !ok {
		return Definition{}, fmt.Errorf("event %q is not registered in the project event catalog", name)
	}
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	var values map[string]any
	if err := json.Unmarshal(payload, &values); err != nil {
		return Definition{}, fmt.Errorf("event %s payload must be a JSON object: %w", name, err)
	}
	for _, field := range definition.RequiredPayloadFields {
		value, exists := values[field]
		if !exists || value == nil || (fmt.Sprint(value) == "") {
			return Definition{}, fmt.Errorf("event %s payload is missing required field %s", name, field)
		}
	}
	return definition, nil
}

// AppendTx is the only supported event write path. The event_outbox trigger
// mirrors the row into project_event_log in the same database transaction.
func AppendTx(
	ctx context.Context,
	tx Execer,
	organizationID string,
	projectID string,
	eventName string,
	aggregateType string,
	aggregateID string,
	payload json.RawMessage,
) error {
	return AppendTxWithRevision(ctx, tx, organizationID, projectID, eventName, aggregateType, aggregateID, nil, payload)
}

func AppendTxWithRevision(
	ctx context.Context,
	tx Execer,
	organizationID string,
	projectID string,
	eventName string,
	aggregateType string,
	aggregateID string,
	aggregateRevision *int64,
	payload json.RawMessage,
) error {
	definition, err := Validate(eventName, payload)
	if err != nil {
		return err
	}
	aggregateType = strings.TrimSpace(aggregateType)
	if aggregateType != definition.AggregateType {
		return fmt.Errorf("event %s aggregate type is %q, want %q", eventName, aggregateType, definition.AggregateType)
	}
	if definition.ScopeType == "project" && strings.TrimSpace(projectID) == "" {
		return fmt.Errorf("event %s requires a project scope", eventName)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO event_outbox(
			organization_id, project_id, event_type, schema_version,
			aggregate_type, aggregate_id, aggregate_revision, payload
		)
		VALUES ($1, NULLIF($2, '')::uuid, $3, $4, $5, NULLIF($6, '')::uuid, $7, $8)
	`, organizationID, projectID, eventName, definition.SchemaVersion, aggregateType, aggregateID, aggregateRevision, payload)
	return err
}
