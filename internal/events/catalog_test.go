package events

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

type recordingExecer struct {
	query string
	args  []any
}

func (exec *recordingExecer) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	exec.query = query
	exec.args = append([]any(nil), args...)
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func TestAppendTxPersistsCatalogContract(t *testing.T) {
	exec := &recordingExecer{}
	payload := json.RawMessage(`{"workflowRunId":"run-1"}`)
	if err := AppendTx(context.Background(), exec, "org-1", "project-1", "workflow.run.completed", "workflow_run", "run-1", payload); err != nil {
		t.Fatalf("AppendTx: %v", err)
	}
	if !strings.Contains(exec.query, "schema_version") || len(exec.args) != 8 || exec.args[3] != 1 {
		t.Fatalf("event insert did not persist catalog schema version: query=%q args=%#v", exec.query, exec.args)
	}
}

func TestAppendTxRejectsUnknownEventAndAggregateDrift(t *testing.T) {
	exec := &recordingExecer{}
	if err := AppendTx(context.Background(), exec, "org-1", "project-1", "unknown.event", "workflow_run", "run-1", json.RawMessage(`{}`)); err == nil {
		t.Fatal("unknown event was accepted")
	}
	if err := AppendTx(context.Background(), exec, "org-1", "project-1", "workflow.run.completed", "script", "run-1", json.RawMessage(`{"workflowRunId":"run-1"}`)); err == nil {
		t.Fatal("aggregate drift was accepted")
	}
}

func TestAppendTxRejectsMissingRequiredPayloadAndProjectScope(t *testing.T) {
	exec := &recordingExecer{}
	if err := AppendTx(context.Background(), exec, "org-1", "project-1", "workflow.run.completed", "workflow_run", "run-1", json.RawMessage(`{}`)); err == nil {
		t.Fatal("missing required payload field was accepted")
	}
	if err := AppendTx(context.Background(), exec, "org-1", "", "workflow.run.completed", "workflow_run", "run-1", json.RawMessage(`{"workflowRunId":"run-1"}`)); err == nil {
		t.Fatal("project-scoped event without project was accepted")
	}
}

func TestCommerceCatalogContractsAcceptCompletePayloads(t *testing.T) {
	for name, definition := range Catalog {
		if !strings.HasPrefix(name, "commerce.") {
			continue
		}
		payload := make(map[string]any, len(definition.RequiredPayloadFields))
		for _, field := range definition.RequiredPayloadFields {
			payload[field] = "test-value"
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal %s payload: %v", name, err)
		}
		exec := &recordingExecer{}
		if err := AppendTx(
			context.Background(), exec, "org-1", "project-1",
			name, definition.AggregateType, "aggregate-1", raw,
		); err != nil {
			t.Errorf("%s rejected its complete catalog payload: %v", name, err)
		}
	}
}
