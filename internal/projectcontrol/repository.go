package projectcontrol

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/events"
	"github.com/Einzieg/cineweave/internal/observability"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultCommandPageSize = 20
	maximumCommandPageSize = 50
	maximumEventPageSize   = 200
)

type Repository struct {
	db *pgxpool.Pool
}

type SyncMutation func(context.Context, pgx.Tx, Command) (json.RawMessage, error)

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Create(ctx context.Context, request CreateCommand) (Command, bool, error) {
	if r == nil || r.db == nil {
		return Command{}, false, fmt.Errorf("project control repository is unavailable")
	}
	if err := request.Validate(); err != nil {
		return Command{}, false, err
	}
	canonicalInput, _, err := canonicalObject(request.Input, 65536)
	if err != nil {
		return Command{}, false, fmt.Errorf("normalize project control input: %w", err)
	}
	inputHash, err := commandRequestHash(canonicalInput, request.Items)
	if err != nil {
		return Command{}, false, err
	}

	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Command{}, false, err
	}
	defer tx.Rollback(ctx)

	commandID := uuid.NewString()
	tag, err := tx.Exec(ctx, `
		INSERT INTO project_control_commands(
			id, organization_id, workspace_id, project_id, actor_user_id,
			controller_type, control_key_id, agent_task_id, agent_step_id,
			action_name, action_version, execution_mode, activity_visibility,
			input, input_hash, idempotency_key, parent_command_id, retry_of_command_id
		)
		VALUES (
			$1, $2, NULLIF($3, '')::uuid, NULLIF($4, '')::uuid, $5,
			$6, NULLIF($7, '')::uuid, NULLIF($8, '')::uuid, NULLIF($9, '')::uuid,
			$10, $11, $12, $13, $14, $15, $16,
			NULLIF($17, '')::uuid, NULLIF($18, '')::uuid
		)
		ON CONFLICT (actor_user_id, controller_type, idempotency_key) DO NOTHING
	`, commandID, request.OrganizationID, request.WorkspaceID, request.ProjectID,
		request.ActorUserID, request.ControllerType, request.ControlKeyID,
		request.AgentTaskID, request.AgentStepID, request.Descriptor.Name,
		request.Descriptor.Version, request.Descriptor.ExecutionMode,
		request.Descriptor.ActivityVisibility, canonicalInput, inputHash,
		request.IdempotencyKey, request.ParentCommandID, request.RetryOfCommandID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "project_control_commands_one_active_retry_idx" {
			return Command{}, false, ErrRetryAlreadyActive
		}
		return Command{}, false, err
	}
	if tag.RowsAffected() == 0 {
		existing, err := getCommandByIdempotencyTx(ctx, tx, request.ActorUserID, request.ControllerType, request.IdempotencyKey)
		if err != nil {
			return Command{}, false, err
		}
		if !sameIdempotentRequest(existing, request, inputHash) {
			observability.RecordProjectControlConflict("idempotency_conflict")
			return Command{}, false, fmt.Errorf("%w: key %q was already used for a different request", ErrIdempotencyConflict, request.IdempotencyKey)
		}
		if err := linkEmbeddedAgentStepTx(ctx, tx, existing, request); err != nil {
			return Command{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Command{}, false, err
		}
		observability.RecordProjectControlConflict("idempotent_replay")
		return existing, true, nil
	}

	for _, item := range request.Items {
		canonicalItemInput, itemHash, err := canonicalObject(item.Input, 65536)
		if err != nil {
			return Command{}, false, fmt.Errorf("normalize command item %s input: %w", item.ItemKey, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO project_control_command_items(
				id, command_id, item_key, stable_ordinal, target_type,
				target_id, target_revision, input, input_hash
			)
			VALUES ($1, $2, $3, $4, $5, NULLIF($6, '')::uuid, $7, $8, $9)
		`, uuid.NewString(), commandID, strings.TrimSpace(item.ItemKey), item.StableOrdinal,
			strings.TrimSpace(item.TargetType), item.TargetID, item.TargetRevision,
			canonicalItemInput, itemHash); err != nil {
			return Command{}, false, err
		}
	}

	command, err := getCommandTx(ctx, tx, commandID, false)
	if err != nil {
		return Command{}, false, err
	}
	if err := linkEmbeddedAgentStepTx(ctx, tx, command, request); err != nil {
		return Command{}, false, err
	}
	if _, err := appendCommandEventTx(ctx, tx, command, "project.control.command.created", map[string]any{
		"actionName":     command.ActionName,
		"controllerType": command.ControllerType,
	}); err != nil {
		return Command{}, false, err
	}
	if command.RetryOfCommandID != "" {
		if _, err := appendCommandEventTx(ctx, tx, command, "project.control.command.retry_created", map[string]any{
			"retryOfCommandId": command.RetryOfCommandID,
		}); err != nil {
			return Command{}, false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Command{}, false, err
	}
	return command, false, nil
}

// ExecuteSync persists the idempotency record, performs a lightweight domain
// mutation, and completes the command in one serializable transaction. It is
// reserved for bounded writes that do not start workflows or call providers.
func (r *Repository) ExecuteSync(ctx context.Context, request CreateCommand, mutation SyncMutation) (Command, bool, error) {
	if r == nil || r.db == nil {
		return Command{}, false, fmt.Errorf("project control repository is unavailable")
	}
	if mutation == nil {
		return Command{}, false, fmt.Errorf("project control sync mutation is required")
	}
	if err := request.Validate(); err != nil {
		return Command{}, false, err
	}
	if request.Descriptor.ExecutionMode != ExecutionModeSync || request.Descriptor.StartsWorkflow || request.Descriptor.Costed {
		return Command{}, false, fmt.Errorf("project control action %s is not a bounded synchronous mutation", request.Descriptor.Name)
	}
	canonicalInput, _, err := canonicalObject(request.Input, 65536)
	if err != nil {
		return Command{}, false, fmt.Errorf("normalize project control input: %w", err)
	}
	inputHash, err := commandRequestHash(canonicalInput, request.Items)
	if err != nil {
		return Command{}, false, err
	}
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Command{}, false, err
	}
	defer tx.Rollback(ctx)

	commandID := uuid.NewString()
	tag, err := tx.Exec(ctx, `
		INSERT INTO project_control_commands(
			id, organization_id, workspace_id, project_id, actor_user_id,
			controller_type, control_key_id, agent_task_id, agent_step_id,
			action_name, action_version, execution_mode, activity_visibility,
			input, input_hash, idempotency_key, parent_command_id, retry_of_command_id
		)
		VALUES (
			$1, $2, NULLIF($3, '')::uuid, NULLIF($4, '')::uuid, $5,
			$6, NULLIF($7, '')::uuid, NULLIF($8, '')::uuid, NULLIF($9, '')::uuid,
			$10, $11, $12, $13, $14, $15, $16,
			NULLIF($17, '')::uuid, NULLIF($18, '')::uuid
		)
		ON CONFLICT (actor_user_id, controller_type, idempotency_key) DO NOTHING
	`, commandID, request.OrganizationID, request.WorkspaceID, request.ProjectID,
		request.ActorUserID, request.ControllerType, request.ControlKeyID,
		request.AgentTaskID, request.AgentStepID, request.Descriptor.Name,
		request.Descriptor.Version, request.Descriptor.ExecutionMode,
		request.Descriptor.ActivityVisibility, canonicalInput, inputHash,
		request.IdempotencyKey, request.ParentCommandID, request.RetryOfCommandID)
	if err != nil {
		return Command{}, false, err
	}
	if tag.RowsAffected() == 0 {
		existing, err := getCommandByIdempotencyTx(ctx, tx, request.ActorUserID, request.ControllerType, request.IdempotencyKey)
		if err != nil {
			return Command{}, false, err
		}
		if !sameIdempotentRequest(existing, request, inputHash) {
			observability.RecordProjectControlConflict("idempotency_conflict")
			return Command{}, false, fmt.Errorf("%w: key %q was already used for a different request", ErrIdempotencyConflict, request.IdempotencyKey)
		}
		if err := linkEmbeddedAgentStepTx(ctx, tx, existing, request); err != nil {
			return Command{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Command{}, false, err
		}
		observability.RecordProjectControlConflict("idempotent_replay")
		return existing, true, nil
	}
	if len(request.Items) != 0 {
		return Command{}, false, fmt.Errorf("bounded synchronous mutations do not support command items")
	}
	command, err := getCommandTx(ctx, tx, commandID, false)
	if err != nil {
		return Command{}, false, err
	}
	if err := linkEmbeddedAgentStepTx(ctx, tx, command, request); err != nil {
		return Command{}, false, err
	}
	if _, err := appendCommandEventTx(ctx, tx, command, "project.control.command.created", map[string]any{
		"actionName": command.ActionName, "controllerType": command.ControllerType,
	}); err != nil {
		return Command{}, false, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE project_control_commands
		SET status = 'running', started_at = now(), revision = revision + 1
		WHERE id = $1 AND revision = $2
	`, command.ID, command.Revision); err != nil {
		return Command{}, false, err
	}
	command, err = getCommandTx(ctx, tx, command.ID, false)
	if err != nil {
		return Command{}, false, err
	}
	if _, err := appendCommandEventTx(ctx, tx, command, "project.control.command.running", map[string]any{
		"phase": "sync_mutation",
	}); err != nil {
		return Command{}, false, err
	}
	output, err := mutation(ctx, tx, command)
	if err != nil {
		return Command{}, false, err
	}
	canonicalOutput, _, err := canonicalObject(output, 65536)
	if err != nil {
		return Command{}, false, fmt.Errorf("normalize project control output: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE project_control_commands
		SET status = 'succeeded', output = $2, completed_at = now(), revision = revision + 1
		WHERE id = $1 AND revision = $3
	`, command.ID, canonicalOutput, command.Revision); err != nil {
		return Command{}, false, err
	}
	command, err = getCommandTx(ctx, tx, command.ID, false)
	if err != nil {
		return Command{}, false, err
	}
	if _, err := appendCommandEventTx(ctx, tx, command, "project.control.command.succeeded", map[string]any{
		"phase": "sync_mutation",
	}); err != nil {
		return Command{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Command{}, false, err
	}
	return command, false, nil
}

func linkEmbeddedAgentStepTx(ctx context.Context, tx pgx.Tx, command Command, request CreateCommand) error {
	if request.ControllerType != ControllerEmbeddedAgent {
		return nil
	}
	tag, err := tx.Exec(ctx, `
		UPDATE agent_steps
		SET project_control_command_id = $1
		WHERE id = $2
		  AND task_id = $3
		  AND (project_control_command_id IS NULL OR project_control_command_id = $1)
	`, command.ID, request.AgentStepID, request.AgentTaskID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("agent step %s cannot be linked to project control command %s", request.AgentStepID, command.ID)
	}
	return nil
}

func (r *Repository) Get(ctx context.Context, commandID string) (Command, error) {
	if r == nil || r.db == nil {
		return Command{}, fmt.Errorf("project control repository is unavailable")
	}
	command, err := scanCommand(r.db.QueryRow(ctx, commandSelectSQL+` WHERE id = $1`, commandID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Command{}, ErrCommandNotFound
	}
	return command, err
}

func (r *Repository) List(ctx context.Context, filter ListCommandsFilter) (CommandPage, error) {
	if r == nil || r.db == nil {
		return CommandPage{}, fmt.Errorf("project control repository is unavailable")
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = defaultCommandPageSize
	}
	if limit > maximumCommandPageSize {
		limit = maximumCommandPageSize
	}
	if filter.BeforeCreatedAt != nil && strings.TrimSpace(filter.BeforeID) == "" {
		return CommandPage{}, fmt.Errorf("command cursor ID is required")
	}
	if filter.ActivityView && (strings.TrimSpace(filter.ActorUserID) == "" || strings.TrimSpace(filter.ProjectID) == "") {
		return CommandPage{}, fmt.Errorf("activity view requires actor and project")
	}
	statuses := make([]string, 0, len(filter.Statuses))
	for _, status := range filter.Statuses {
		if !status.Valid() {
			return CommandPage{}, fmt.Errorf("command status %q is invalid", status)
		}
		statuses = append(statuses, string(status))
	}
	rows, err := r.db.Query(ctx, commandSelectSQL+`
		WHERE ($1 = '' OR actor_user_id = $1::uuid)
		  AND ($2 = '' OR organization_id = $2::uuid)
		  AND ($3 = '' OR project_id = $3::uuid)
		  AND ($4 = '' OR controller_type = $4)
		  AND (cardinality($5::text[]) = 0 OR status = ANY($5::text[]))
		  AND ($6::timestamptz IS NULL OR created_at >= $6::timestamptz)
		  AND (
			$7::timestamptz IS NULL
			OR (created_at, id) < ($7::timestamptz, NULLIF($8, '')::uuid)
		  )
		  AND (NOT $9::boolean OR activity_visibility = 'primary')
		  AND (
			NOT $9::boolean
			OR status NOT IN ('succeeded', 'partial_succeeded', 'failed', 'cancelled')
			OR COALESCE(completed_at, updated_at) > COALESCE((
				SELECT cleared_terminal_through
				FROM workflow_activity_views
				WHERE organization_id = project_control_commands.organization_id
				  AND project_id = project_control_commands.project_id
				  AND user_id = $1::uuid
			), '-infinity'::timestamptz)
		  )
		ORDER BY created_at DESC, id DESC
		LIMIT $10
	`, filter.ActorUserID, filter.OrganizationID, filter.ProjectID, filter.ControllerType, statuses,
		filter.CreatedAfter, filter.BeforeCreatedAt, filter.BeforeID, filter.ActivityView, limit+1)
	if err != nil {
		return CommandPage{}, err
	}
	defer rows.Close()
	commands := make([]Command, 0, limit+1)
	for rows.Next() {
		command, err := scanCommand(rows)
		if err != nil {
			return CommandPage{}, err
		}
		commands = append(commands, command)
	}
	if err := rows.Err(); err != nil {
		return CommandPage{}, err
	}
	page := CommandPage{Commands: commands}
	if len(commands) > limit {
		last := commands[limit-1]
		page.Commands = commands[:limit]
		page.NextCursor = &CommandCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	return page, nil
}

func (r *Repository) Children(ctx context.Context, parentCommandID string) ([]Command, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("project control repository is unavailable")
	}
	parentCommandID = strings.TrimSpace(parentCommandID)
	if parentCommandID == "" {
		return nil, fmt.Errorf("parent command ID is required")
	}
	rows, err := r.db.Query(ctx, commandSelectSQL+`
		WHERE parent_command_id = $1
		ORDER BY created_at, id
	`, parentCommandID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	commands := make([]Command, 0)
	for rows.Next() {
		command, err := scanCommand(rows)
		if err != nil {
			return nil, err
		}
		commands = append(commands, command)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return commands, nil
}

func (r *Repository) WorkflowRunIDsByCommand(ctx context.Context, commandIDs []string) (map[string][]string, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("project control repository is unavailable")
	}
	if len(commandIDs) == 0 {
		return map[string][]string{}, nil
	}
	rows, err := r.db.Query(ctx, `
		SELECT command_id::text, workflow_run_id::text
		FROM project_control_command_workflows
		WHERE command_id = ANY($1::uuid[])
		  AND workflow_run_id IS NOT NULL
		ORDER BY command_id, created_at, id
	`, commandIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string][]string, len(commandIDs))
	for rows.Next() {
		var commandID, workflowRunID string
		if err := rows.Scan(&commandID, &workflowRunID); err != nil {
			return nil, err
		}
		result[commandID] = append(result[commandID], workflowRunID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *Repository) ActiveCount(ctx context.Context, actorUserID string) (int, error) {
	if r == nil || r.db == nil {
		return 0, fmt.Errorf("project control repository is unavailable")
	}
	if strings.TrimSpace(actorUserID) == "" {
		return 0, fmt.Errorf("project control actor is required")
	}
	var count int
	err := r.db.QueryRow(ctx, `
		SELECT count(*)
		FROM project_control_commands
		WHERE actor_user_id = $1
		  AND status IN ('queued', 'running', 'waiting_workflow', 'waiting_input')
	`, actorUserID).Scan(&count)
	return count, err
}

func (r *Repository) ListActiveForAgentTask(ctx context.Context, agentTaskID string) ([]Command, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("project control repository is unavailable")
	}
	agentTaskID = strings.TrimSpace(agentTaskID)
	if agentTaskID == "" {
		return nil, fmt.Errorf("agent task ID is required")
	}
	rows, err := r.db.Query(ctx, commandSelectSQL+`
		WHERE agent_task_id = $1
		  AND status IN ('queued', 'running', 'waiting_workflow', 'waiting_input')
		ORDER BY created_at, id
	`, agentTaskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	commands := make([]Command, 0)
	for rows.Next() {
		command, err := scanCommand(rows)
		if err != nil {
			return nil, err
		}
		commands = append(commands, command)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return commands, nil
}

func (r *Repository) RequestCancellation(ctx context.Context, request RequestCancellation) (Command, bool, error) {
	if r == nil || r.db == nil {
		return Command{}, false, fmt.Errorf("project control repository is unavailable")
	}
	request.CommandID = strings.TrimSpace(request.CommandID)
	request.ActorUserID = strings.TrimSpace(request.ActorUserID)
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	request.Reason = strings.TrimSpace(request.Reason)
	if request.CommandID == "" || request.ActorUserID == "" || request.ExpectedRevision < 1 ||
		request.IdempotencyKey == "" || len(request.IdempotencyKey) > 200 || len(request.Reason) > 1000 {
		return Command{}, false, fmt.Errorf("project control cancellation request is invalid")
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Command{}, false, err
	}
	defer tx.Rollback(ctx)
	current, err := getCommandTx(ctx, tx, request.CommandID, true)
	if err != nil {
		return Command{}, false, err
	}
	if current.Terminal() {
		if err := tx.Commit(ctx); err != nil {
			return Command{}, false, err
		}
		return current, true, nil
	}
	if current.CancellationRequestedAt != nil {
		if err := tx.Commit(ctx); err != nil {
			return Command{}, false, err
		}
		return current, true, nil
	}
	if current.Revision != request.ExpectedRevision {
		return Command{}, false, ErrRevisionConflict
	}
	var workflowCount int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM project_control_command_workflows WHERE command_id = $1
	`, current.ID).Scan(&workflowCount); err != nil {
		return Command{}, false, err
	}
	immediate := current.Status == CommandQueued && workflowCount == 0 && current.LeaseOwner == ""
	if _, err := tx.Exec(ctx, `
		UPDATE project_control_commands
		SET cancellation_requested_at = now(),
		    cancellation_requested_by_user_id = $2,
		    cancellation_idempotency_key = $3,
		    cancellation_reason = NULLIF($4, ''),
		    status = CASE WHEN $5 THEN 'cancelled' ELSE status END,
		    completed_at = CASE WHEN $5 THEN now() ELSE completed_at END,
		    next_reconcile_at = CASE WHEN $5 THEN NULL ELSE now() END,
		    lease_owner = CASE WHEN $5 THEN NULL ELSE lease_owner END,
		    lease_expires_at = CASE WHEN $5 THEN NULL ELSE lease_expires_at END,
		    revision = revision + 1
		WHERE id = $1 AND revision = $6
	`, current.ID, request.ActorUserID, request.IdempotencyKey, request.Reason,
		immediate, request.ExpectedRevision); err != nil {
		return Command{}, false, err
	}
	updated, err := getCommandTx(ctx, tx, current.ID, false)
	if err != nil {
		return Command{}, false, err
	}
	eventType := "project.control.command.progress"
	if immediate {
		eventType = "project.control.command.cancelled"
	}
	if _, err := appendCommandEventTx(ctx, tx, updated, eventType, map[string]any{
		"phase": "cancellation_requested", "requestedByUserId": request.ActorUserID,
		"immediate": immediate,
	}); err != nil {
		return Command{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Command{}, false, err
	}
	return updated, false, nil
}

func (r *Repository) Retry(ctx context.Context, request RetryCommand) (Command, bool, error) {
	if r == nil || r.db == nil {
		return Command{}, false, fmt.Errorf("project control repository is unavailable")
	}
	request.CommandID = strings.TrimSpace(request.CommandID)
	request.ActorUserID = strings.TrimSpace(request.ActorUserID)
	request.ControlKeyID = strings.TrimSpace(request.ControlKeyID)
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	if request.CommandID == "" || request.ActorUserID == "" || request.ExpectedRevision < 1 ||
		request.IdempotencyKey == "" || len(request.IdempotencyKey) > 200 {
		return Command{}, false, fmt.Errorf("project control retry request is invalid")
	}
	original, err := r.Get(ctx, request.CommandID)
	if err != nil {
		return Command{}, false, err
	}
	if original.Revision != request.ExpectedRevision {
		return Command{}, false, ErrRevisionConflict
	}
	if original.Status != CommandFailed && original.Status != CommandPartialSucceeded {
		return Command{}, false, ErrRetryUnavailable
	}
	if original.ActionName != request.Descriptor.Name || original.ActionVersion != request.Descriptor.Version ||
		original.ExecutionMode != request.Descriptor.ExecutionMode {
		return Command{}, false, fmt.Errorf("retry action contract does not match original command")
	}
	items, err := r.Items(ctx, original.ID)
	if err != nil {
		return Command{}, false, err
	}
	retryItems := make([]CreateCommandItem, 0, len(items))
	for _, item := range items {
		if item.Status != "failed" || !item.Retryable {
			continue
		}
		retryItems = append(retryItems, CreateCommandItem{
			ItemKey: item.ItemKey, StableOrdinal: item.StableOrdinal,
			TargetType: item.TargetType, TargetID: item.TargetID,
			TargetRevision: item.TargetRevision, Input: item.Input,
		})
	}
	if len(items) > 0 && len(retryItems) == 0 {
		return Command{}, false, ErrRetryUnavailable
	}
	return r.Create(ctx, CreateCommand{
		OrganizationID: original.OrganizationID, WorkspaceID: original.WorkspaceID,
		ProjectID: original.ProjectID, ActorUserID: request.ActorUserID,
		ControllerType: request.ControllerType, ControlKeyID: request.ControlKeyID,
		Descriptor: request.Descriptor, Input: original.Input,
		IdempotencyKey: request.IdempotencyKey, RetryOfCommandID: original.ID,
		Items: retryItems,
	})
}

func (r *Repository) Items(ctx context.Context, commandID string) ([]CommandItem, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id::text, command_id::text, item_key, stable_ordinal, target_type,
		       COALESCE(target_id::text, ''), target_revision, input, input_hash, status,
		       retryable, output, COALESCE(error_code, ''), COALESCE(error_message, ''),
		       created_at, started_at, completed_at
		FROM project_control_command_items
		WHERE command_id = $1
		ORDER BY stable_ordinal NULLS LAST, created_at, id
	`, commandID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]CommandItem, 0)
	for rows.Next() {
		var item CommandItem
		var stableOrdinal pgtype.Int4
		var targetRevision pgtype.Int8
		var input, output []byte
		var startedAt, completedAt pgtype.Timestamptz
		if err := rows.Scan(&item.ID, &item.CommandID, &item.ItemKey, &stableOrdinal,
			&item.TargetType, &item.TargetID, &targetRevision, &input, &item.InputHash,
			&item.Status, &item.Retryable, &output, &item.ErrorCode, &item.ErrorMessage,
			&item.CreatedAt, &startedAt, &completedAt); err != nil {
			return nil, err
		}
		item.Output = cloneRawMessage(output)
		item.Input = cloneRawMessage(input)
		if stableOrdinal.Valid {
			value := int(stableOrdinal.Int32)
			item.StableOrdinal = &value
		}
		if targetRevision.Valid {
			value := targetRevision.Int64
			item.TargetRevision = &value
		}
		item.StartedAt = timestampPointer(startedAt)
		item.CompletedAt = timestampPointer(completedAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ApplyItemResults(ctx context.Context, commandID string, expectedRevision int64, results []ItemResult) (Command, error) {
	if len(results) == 0 {
		return r.Get(ctx, commandID)
	}
	seen := make(map[string]struct{}, len(results))
	for _, result := range results {
		itemID := strings.TrimSpace(result.CommandItemID)
		if itemID == "" {
			return Command{}, fmt.Errorf("command item ID is required")
		}
		if _, exists := seen[itemID]; exists {
			return Command{}, fmt.Errorf("command item %s appears more than once", itemID)
		}
		seen[itemID] = struct{}{}
		if !validItemStatus(result.Status) {
			return Command{}, fmt.Errorf("command item status %q is invalid", result.Status)
		}
		if result.Status == "failed" && strings.TrimSpace(result.ErrorCode) == "" {
			return Command{}, fmt.Errorf("failed command item %s requires an error code", itemID)
		}
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Command{}, err
	}
	defer tx.Rollback(ctx)
	command, err := getCommandTx(ctx, tx, commandID, true)
	if err != nil {
		return Command{}, err
	}
	if command.Revision != expectedRevision {
		return Command{}, ErrRevisionConflict
	}
	if command.Terminal() {
		return Command{}, fmt.Errorf("terminal command %s cannot update items", commandID)
	}

	for _, result := range results {
		var currentStatus string
		if err := tx.QueryRow(ctx, `
			SELECT status
			FROM project_control_command_items
			WHERE id = $1 AND command_id = $2
			FOR UPDATE
		`, result.CommandItemID, commandID).Scan(&currentStatus); errors.Is(err, pgx.ErrNoRows) {
			return Command{}, fmt.Errorf("command item %s was not found", result.CommandItemID)
		} else if err != nil {
			return Command{}, err
		}
		if !validItemTransition(currentStatus, result.Status) {
			return Command{}, fmt.Errorf("invalid command item transition %s -> %s", currentStatus, result.Status)
		}
		output := json.RawMessage(`{}`)
		if len(result.Output) > 0 {
			output, _, err = canonicalObject(result.Output, 65536)
			if err != nil {
				return Command{}, fmt.Errorf("normalize command item %s output: %w", result.CommandItemID, err)
			}
		}
		terminal := itemStatusTerminal(result.Status)
		errorCode := ""
		errorMessage := ""
		retryable := false
		if result.Status == "failed" {
			errorCode = strings.TrimSpace(result.ErrorCode)
			errorMessage = strings.TrimSpace(result.ErrorMessage)
			retryable = result.Retryable
		}
		if _, err := tx.Exec(ctx, `
			UPDATE project_control_command_items
			SET status = $3, retryable = $4, output = $5,
			    error_code = NULLIF($6, ''), error_message = NULLIF($7, ''),
			    started_at = CASE WHEN $3 <> 'queued' THEN COALESCE(started_at, now()) ELSE started_at END,
			    completed_at = CASE WHEN $8 THEN COALESCE(completed_at, now()) ELSE NULL END
			WHERE id = $1 AND command_id = $2
		`, result.CommandItemID, commandID, result.Status, retryable, output,
			errorCode, errorMessage, terminal); err != nil {
			return Command{}, err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE project_control_commands
		SET revision = revision + 1
		WHERE id = $1 AND revision = $2
	`, commandID, expectedRevision); err != nil {
		return Command{}, err
	}
	updated, err := getCommandTx(ctx, tx, commandID, false)
	if err != nil {
		return Command{}, err
	}
	if _, err := appendCommandEventTx(ctx, tx, updated, "project.control.command.progress", map[string]any{
		"updatedItemCount": len(results),
	}); err != nil {
		return Command{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Command{}, err
	}
	return updated, nil
}

func (r *Repository) RescheduleReconcile(ctx context.Context, commandID string, expectedRevision int64, nextReconcileAt time.Time) (Command, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Command{}, err
	}
	defer tx.Rollback(ctx)
	current, err := getCommandTx(ctx, tx, commandID, true)
	if err != nil {
		return Command{}, err
	}
	if current.Revision != expectedRevision {
		return Command{}, ErrRevisionConflict
	}
	if current.Status != CommandWaitingWorkflow {
		return Command{}, fmt.Errorf("command %s cannot be rescheduled while %s", commandID, current.Status)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE project_control_commands
		SET next_reconcile_at = $2, lease_owner = NULL, lease_expires_at = NULL,
		    revision = revision + 1
		WHERE id = $1 AND revision = $3
	`, commandID, nextReconcileAt, expectedRevision); err != nil {
		return Command{}, err
	}
	updated, err := getCommandTx(ctx, tx, commandID, false)
	if err != nil {
		return Command{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Command{}, err
	}
	return updated, nil
}

func (r *Repository) Events(ctx context.Context, commandID string, afterSequence int64, limit int) ([]CommandEvent, error) {
	if limit <= 0 || limit > maximumEventPageSize {
		limit = maximumEventPageSize
	}
	rows, err := r.db.Query(ctx, `
		SELECT sequence, command_id::text, event_type, payload, created_at
		FROM project_control_command_events
		WHERE command_id = $1 AND sequence > $2
		ORDER BY sequence
		LIMIT $3
	`, commandID, afterSequence, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	eventsPage := make([]CommandEvent, 0)
	for rows.Next() {
		var event CommandEvent
		var payload []byte
		if err := rows.Scan(&event.Sequence, &event.CommandID, &event.EventType, &payload, &event.CreatedAt); err != nil {
			return nil, err
		}
		event.Payload = cloneRawMessage(payload)
		eventsPage = append(eventsPage, event)
	}
	return eventsPage, rows.Err()
}

func (r *Repository) Transition(ctx context.Context, request TransitionCommand) (Command, error) {
	if !request.Status.Valid() {
		return Command{}, fmt.Errorf("command status %q is invalid", request.Status)
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Command{}, err
	}
	defer tx.Rollback(ctx)
	current, err := getCommandTx(ctx, tx, request.CommandID, true)
	if err != nil {
		return Command{}, err
	}
	if current.Revision != request.ExpectedRevision {
		observability.RecordProjectControlConflict("revision_conflict")
		return Command{}, ErrRevisionConflict
	}
	output := current.Output
	if len(request.Output) > 0 {
		output, _, err = canonicalObject(request.Output, 65536)
		if err != nil {
			return Command{}, fmt.Errorf("normalize project control output: %w", err)
		}
	}
	if request.Status == CommandFailed && strings.TrimSpace(request.ErrorCode) == "" {
		return Command{}, fmt.Errorf("failed command requires an error code")
	}
	if _, err := tx.Exec(ctx, `
		UPDATE project_control_commands
		SET status = $2,
		    output = $3,
		    error_code = NULLIF($4, ''),
		    error_message = NULLIF($5, ''),
		    next_reconcile_at = $6,
		    started_at = CASE WHEN $2 = 'running' THEN COALESCE(started_at, now()) ELSE started_at END,
		    completed_at = CASE
		        WHEN $2 IN ('succeeded', 'partial_succeeded', 'failed', 'cancelled') THEN COALESCE(completed_at, now())
		        ELSE NULL
		    END,
		    lease_owner = CASE WHEN $2 = 'running' THEN lease_owner ELSE NULL END,
		    lease_expires_at = CASE WHEN $2 = 'running' THEN lease_expires_at ELSE NULL END,
		    revision = revision + 1
		WHERE id = $1 AND revision = $7
	`, request.CommandID, request.Status, output, request.ErrorCode,
		request.ErrorMessage, request.NextReconcileAt, request.ExpectedRevision); err != nil {
		return Command{}, err
	}
	updated, err := getCommandTx(ctx, tx, request.CommandID, false)
	if err != nil {
		return Command{}, err
	}
	eventType := strings.TrimSpace(request.EventType)
	if eventType == "" {
		eventType = commandStatusEvent(request.Status)
	}
	if _, err := appendCommandEventTx(ctx, tx, updated, eventType, request.EventPayload); err != nil {
		return Command{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Command{}, err
	}
	if updated.Terminal() {
		observability.RecordProjectControlTerminal(
			updated.ActionName,
			string(updated.ControllerType),
			string(updated.Status),
			time.Since(updated.CreatedAt),
		)
	}
	return updated, nil
}

func (r *Repository) ClaimDispatch(ctx context.Context, owner, releaseID string, leaseDuration time.Duration) (*Claim, error) {
	owner = strings.TrimSpace(owner)
	releaseID = strings.TrimSpace(releaseID)
	if owner == "" || releaseID == "" || leaseDuration <= 0 {
		return nil, fmt.Errorf("claim owner, release ID, and positive lease duration are required")
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var commandID string
	var reclaimed bool
	err = tx.QueryRow(ctx, `
		SELECT id::text, status = 'running'
		FROM project_control_commands
		WHERE (
			status = 'queued' AND cancellation_requested_at IS NULL
			AND (lease_expires_at IS NULL OR lease_expires_at <= now())
		) OR (
			status = 'running' AND cancellation_requested_at IS NULL
			AND (lease_expires_at IS NULL OR lease_expires_at <= now())
		)
		ORDER BY created_at, id
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`).Scan(&commandID, &reclaimed)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, tx.Commit(ctx)
	}
	if err != nil {
		return nil, err
	}
	leaseIdentity := uuid.NewString()
	if _, err := tx.Exec(ctx, `
		UPDATE project_control_commands
		SET status = 'running', lease_owner = $2,
		    lease_expires_at = now() + $3::interval,
		    worker_release_id = $4,
		    started_at = COALESCE(started_at, now()),
		    next_reconcile_at = NULL,
		    revision = revision + 1
		WHERE id = $1
	`, commandID, owner, intervalLiteral(leaseDuration), releaseID); err != nil {
		return nil, err
	}
	var attemptNumber int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(attempt_number), 0) + 1
		FROM project_control_command_attempts
		WHERE command_id = $1 AND command_item_id IS NULL AND attempt_kind = 'dispatch'
	`, commandID).Scan(&attemptNumber); err != nil {
		return nil, err
	}
	attemptID := uuid.NewString()
	if _, err := tx.Exec(ctx, `
		INSERT INTO project_control_command_attempts(
			id, command_id, attempt_number, attempt_kind, status,
			worker_release_id, lease_identity
		)
		VALUES ($1, $2, $3, 'dispatch', 'running', $4, $5)
	`, attemptID, commandID, attemptNumber, releaseID, leaseIdentity); err != nil {
		return nil, err
	}
	command, err := getCommandTx(ctx, tx, commandID, false)
	if err != nil {
		return nil, err
	}
	if _, err := appendCommandEventTx(ctx, tx, command, "project.control.command.running", map[string]any{
		"attemptNumber": attemptNumber,
		"reclaimed":     reclaimed,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &Claim{
		Command: command, AttemptID: attemptID, AttemptNumber: attemptNumber,
		AttemptKind: "dispatch", LeaseIdentity: leaseIdentity, Reclaimed: reclaimed,
	}, nil
}

func (r *Repository) ClaimReconcile(ctx context.Context, owner, releaseID string, leaseDuration time.Duration) (*Claim, error) {
	owner = strings.TrimSpace(owner)
	releaseID = strings.TrimSpace(releaseID)
	if owner == "" || releaseID == "" || leaseDuration <= 0 {
		return nil, fmt.Errorf("claim owner, release ID, and positive lease duration are required")
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var commandID string
	err = tx.QueryRow(ctx, `
		SELECT id::text
		FROM project_control_commands
		WHERE (status = 'waiting_workflow'
		       OR (cancellation_requested_at IS NOT NULL
		           AND status IN ('running', 'waiting_workflow', 'waiting_input')))
		  AND COALESCE(next_reconcile_at, updated_at) <= now()
		  AND (lease_expires_at IS NULL OR lease_expires_at <= now())
		ORDER BY COALESCE(next_reconcile_at, updated_at), id
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`).Scan(&commandID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, tx.Commit(ctx)
	}
	if err != nil {
		return nil, err
	}
	leaseIdentity := uuid.NewString()
	if _, err := tx.Exec(ctx, `
		UPDATE project_control_commands
		SET lease_owner = $2, lease_expires_at = now() + $3::interval,
		    worker_release_id = $4, revision = revision + 1
		WHERE id = $1
	`, commandID, owner, intervalLiteral(leaseDuration), releaseID); err != nil {
		return nil, err
	}
	var attemptNumber int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(attempt_number), 0) + 1
		FROM project_control_command_attempts
		WHERE command_id = $1 AND command_item_id IS NULL AND attempt_kind = 'reconcile'
	`, commandID).Scan(&attemptNumber); err != nil {
		return nil, err
	}
	attemptID := uuid.NewString()
	if _, err := tx.Exec(ctx, `
		INSERT INTO project_control_command_attempts(
			id, command_id, attempt_number, attempt_kind, status,
			worker_release_id, lease_identity
		)
		VALUES ($1, $2, $3, 'reconcile', 'running', $4, $5)
	`, attemptID, commandID, attemptNumber, releaseID, leaseIdentity); err != nil {
		return nil, err
	}
	command, err := getCommandTx(ctx, tx, commandID, false)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &Claim{
		Command: command, AttemptID: attemptID, AttemptNumber: attemptNumber,
		AttemptKind: "reconcile", LeaseIdentity: leaseIdentity,
	}, nil
}

func (r *Repository) WorkflowLinks(ctx context.Context, commandID string) ([]WorkflowLink, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id::text, COALESCE(command_item_id::text, ''),
		       COALESCE(workflow_run_id::text, ''), temporal_workflow_id,
		       COALESCE(temporal_run_id, ''), relation_type
		FROM project_control_command_workflows
		WHERE command_id = $1
		ORDER BY created_at, id
	`, commandID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	links := make([]WorkflowLink, 0)
	for rows.Next() {
		var link WorkflowLink
		if err := rows.Scan(&link.ID, &link.CommandItemID, &link.WorkflowRunID,
			&link.TemporalWorkflowID, &link.TemporalRunID, &link.RelationType); err != nil {
			return nil, err
		}
		links = append(links, link)
	}
	return links, rows.Err()
}

func (r *Repository) FinishAttempt(ctx context.Context, attemptID, status, errorCode, errorMessage string) error {
	if status != "succeeded" && status != "failed" && status != "cancelled" {
		return fmt.Errorf("attempt terminal status %q is invalid", status)
	}
	if status == "failed" && strings.TrimSpace(errorCode) == "" {
		return fmt.Errorf("failed attempt requires an error code")
	}
	tag, err := r.db.Exec(ctx, `
		UPDATE project_control_command_attempts
		SET status = $2, error_code = NULLIF($3, ''), error_message = NULLIF($4, ''),
		    completed_at = now()
		WHERE id = $1 AND status = 'running'
	`, attemptID, status, errorCode, errorMessage)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrCommandNotFound
	}
	return nil
}

func (r *Repository) AttachWorkflow(ctx context.Context, commandID string, expectedRevision int64, link WorkflowLink, nextReconcileAt time.Time) (Command, error) {
	return r.AttachWorkflows(ctx, commandID, expectedRevision, []WorkflowLink{link}, nextReconcileAt)
}

func (r *Repository) AttachWorkflows(ctx context.Context, commandID string, expectedRevision int64, links []WorkflowLink, nextReconcileAt time.Time) (Command, error) {
	if len(links) == 0 {
		return Command{}, fmt.Errorf("at least one workflow link is required")
	}
	for _, link := range links {
		if strings.TrimSpace(link.TemporalWorkflowID) == "" || strings.TrimSpace(link.RelationType) == "" {
			return Command{}, fmt.Errorf("temporal workflow ID and relation type are required")
		}
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Command{}, err
	}
	defer tx.Rollback(ctx)
	current, err := getCommandTx(ctx, tx, commandID, true)
	if err != nil {
		return Command{}, err
	}
	if current.Revision != expectedRevision {
		return Command{}, ErrRevisionConflict
	}
	if current.Status != CommandRunning && current.Status != CommandWaitingWorkflow {
		return Command{}, fmt.Errorf("command %s cannot attach a workflow while %s", commandID, current.Status)
	}
	inserted := 0
	workflowRunIDs := make([]string, 0, len(links))
	for _, link := range links {
		tag, err := tx.Exec(ctx, `
			INSERT INTO project_control_command_workflows(
				id, command_id, command_item_id, workflow_run_id,
				temporal_workflow_id, temporal_run_id, relation_type
			)
			VALUES (
				$1, $2, NULLIF($3, '')::uuid, NULLIF($4, '')::uuid,
				$5, NULLIF($6, ''), $7
			)
			ON CONFLICT (command_id, temporal_workflow_id) DO NOTHING
		`, uuid.NewString(), commandID, link.CommandItemID, link.WorkflowRunID,
			link.TemporalWorkflowID, link.TemporalRunID, link.RelationType)
		if err != nil {
			return Command{}, err
		}
		insertedLink := tag.RowsAffected() > 0
		if insertedLink && link.CommandItemID != "" {
			itemTag, err := tx.Exec(ctx, `
				UPDATE project_control_command_items
				SET status = 'waiting_workflow', started_at = COALESCE(started_at, now()),
				    completed_at = NULL, retryable = false,
				    error_code = NULL, error_message = NULL
				WHERE id = $1 AND command_id = $2
				  AND status IN ('queued', 'running', 'waiting_workflow')
			`, link.CommandItemID, commandID)
			if err != nil {
				return Command{}, err
			}
			if itemTag.RowsAffected() == 0 {
				return Command{}, fmt.Errorf("command item %s cannot attach a workflow", link.CommandItemID)
			}
		}
		inserted += int(tag.RowsAffected())
		if link.WorkflowRunID != "" {
			workflowRunIDs = append(workflowRunIDs, link.WorkflowRunID)
		}
	}
	if inserted == 0 {
		if err := tx.Commit(ctx); err != nil {
			return Command{}, err
		}
		return current, nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE project_control_commands
		SET status = 'waiting_workflow', lease_owner = NULL, lease_expires_at = NULL,
		    next_reconcile_at = $2, revision = revision + 1
		WHERE id = $1 AND revision = $3
	`, commandID, nextReconcileAt, expectedRevision); err != nil {
		return Command{}, err
	}
	updated, err := getCommandTx(ctx, tx, commandID, false)
	if err != nil {
		return Command{}, err
	}
	eventType := "project.control.command.progress"
	if current.Status == CommandRunning {
		eventType = "project.control.command.waiting_workflow"
	}
	if _, err := appendCommandEventTx(ctx, tx, updated, eventType, map[string]any{
		"workflowCount":  len(links),
		"workflowRunIds": workflowRunIDs,
	}); err != nil {
		return Command{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Command{}, err
	}
	return updated, nil
}

const commandSelectSQL = `
	SELECT id::text, organization_id::text, COALESCE(workspace_id::text, ''),
	       COALESCE(project_id::text, ''), actor_user_id::text, controller_type,
	       COALESCE(control_key_id::text, ''), COALESCE(agent_task_id::text, ''),
	       COALESCE(agent_step_id::text, ''), action_name, action_version,
	       execution_mode, activity_visibility, input, input_hash, idempotency_key,
	       status, output, COALESCE(error_code, ''), COALESCE(error_message, ''),
	       COALESCE(parent_command_id::text, ''), COALESCE(retry_of_command_id::text, ''),
	       cancellation_requested_at, COALESCE(cancellation_requested_by_user_id::text, ''),
	       COALESCE(cancellation_idempotency_key, ''), COALESCE(cancellation_reason, ''),
	       COALESCE(lease_owner, ''), lease_expires_at, next_reconcile_at,
	       COALESCE(worker_release_id, ''), created_at, updated_at, started_at,
	       completed_at, revision
	FROM project_control_commands
`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanCommand(row rowScanner) (Command, error) {
	var command Command
	var input, output []byte
	var cancellationRequestedAt, leaseExpiresAt, nextReconcileAt, startedAt, completedAt pgtype.Timestamptz
	if err := row.Scan(
		&command.ID, &command.OrganizationID, &command.WorkspaceID, &command.ProjectID,
		&command.ActorUserID, &command.ControllerType, &command.ControlKeyID,
		&command.AgentTaskID, &command.AgentStepID, &command.ActionName,
		&command.ActionVersion, &command.ExecutionMode, &command.ActivityVisibility,
		&input, &command.InputHash, &command.IdempotencyKey, &command.Status, &output,
		&command.ErrorCode, &command.ErrorMessage, &command.ParentCommandID,
		&command.RetryOfCommandID, &cancellationRequestedAt,
		&command.CancellationRequestedByUserID, &command.CancellationIdempotencyKey,
		&command.CancellationReason, &command.LeaseOwner, &leaseExpiresAt,
		&nextReconcileAt, &command.WorkerReleaseID, &command.CreatedAt, &command.UpdatedAt,
		&startedAt, &completedAt, &command.Revision,
	); err != nil {
		return Command{}, err
	}
	command.Input = cloneRawMessage(input)
	command.Output = cloneRawMessage(output)
	command.CancellationRequestedAt = timestampPointer(cancellationRequestedAt)
	command.LeaseExpiresAt = timestampPointer(leaseExpiresAt)
	command.NextReconcileAt = timestampPointer(nextReconcileAt)
	command.StartedAt = timestampPointer(startedAt)
	command.CompletedAt = timestampPointer(completedAt)
	return command, nil
}

func getCommandTx(ctx context.Context, tx pgx.Tx, commandID string, lock bool) (Command, error) {
	query := commandSelectSQL + ` WHERE id = $1`
	if lock {
		query += ` FOR UPDATE`
	}
	command, err := scanCommand(tx.QueryRow(ctx, query, commandID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Command{}, ErrCommandNotFound
	}
	return command, err
}

func getCommandByIdempotencyTx(ctx context.Context, tx pgx.Tx, actorUserID string, controllerType ControllerType, key string) (Command, error) {
	command, err := scanCommand(tx.QueryRow(ctx, commandSelectSQL+`
		WHERE actor_user_id = $1 AND controller_type = $2 AND idempotency_key = $3
		FOR SHARE
	`, actorUserID, controllerType, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return Command{}, ErrCommandNotFound
	}
	return command, err
}

func appendCommandEventTx(ctx context.Context, tx pgx.Tx, command Command, eventType string, additions map[string]any) (int64, error) {
	payload := make(map[string]any, len(additions)+3)
	for key, value := range additions {
		payload[key] = value
	}
	payload["commandId"] = command.ID
	payload["status"] = command.Status
	var sequence int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO project_control_command_events(command_id, event_type, payload)
		VALUES ($1, $2, '{}'::jsonb)
		RETURNING sequence
	`, command.ID, eventType).Scan(&sequence); err != nil {
		return 0, err
	}
	payload["sequence"] = sequence
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	if len(raw) > 32768 {
		return 0, fmt.Errorf("project control command event payload exceeds 32768 bytes")
	}
	if _, err := tx.Exec(ctx, `
		UPDATE project_control_command_events SET payload = $2 WHERE sequence = $1
	`, sequence, raw); err != nil {
		return 0, err
	}
	revision := command.Revision
	if err := events.AppendTxWithRevision(ctx, tx, command.OrganizationID, command.ProjectID,
		eventType, "project_control_command", command.ID, &revision, raw); err != nil {
		return 0, err
	}
	return sequence, nil
}

func canonicalObject(raw json.RawMessage, maximumBytes int) (json.RawMessage, string, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, "", fmt.Errorf("value must be a JSON object: %w", err)
	}
	if value == nil {
		return nil, "", fmt.Errorf("value must be a JSON object")
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, "", err
	}
	if len(canonical) > maximumBytes {
		return nil, "", fmt.Errorf("JSON object exceeds %d bytes", maximumBytes)
	}
	hash := sha256.Sum256(canonical)
	return canonical, hex.EncodeToString(hash[:]), nil
}

func commandRequestHash(input json.RawMessage, items []CreateCommandItem) (string, error) {
	type hashedItem struct {
		ItemKey        string `json:"itemKey"`
		StableOrdinal  *int   `json:"stableOrdinal,omitempty"`
		TargetType     string `json:"targetType"`
		TargetID       string `json:"targetId,omitempty"`
		TargetRevision *int64 `json:"targetRevision,omitempty"`
		InputHash      string `json:"inputHash"`
	}
	payload := struct {
		Input json.RawMessage `json:"input"`
		Items []hashedItem    `json:"items"`
	}{Input: input, Items: make([]hashedItem, 0, len(items))}
	for _, item := range items {
		_, itemHash, err := canonicalObject(item.Input, 65536)
		if err != nil {
			return "", fmt.Errorf("normalize command item %s input: %w", item.ItemKey, err)
		}
		payload.Items = append(payload.Items, hashedItem{
			ItemKey: strings.TrimSpace(item.ItemKey), StableOrdinal: item.StableOrdinal,
			TargetType: strings.TrimSpace(item.TargetType), TargetID: strings.TrimSpace(item.TargetID),
			TargetRevision: item.TargetRevision, InputHash: itemHash,
		})
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:]), nil
}

func sameIdempotentRequest(existing Command, request CreateCommand, inputHash string) bool {
	return existing.OrganizationID == request.OrganizationID &&
		existing.WorkspaceID == request.WorkspaceID &&
		existing.ProjectID == request.ProjectID &&
		existing.ActorUserID == request.ActorUserID &&
		existing.ControllerType == request.ControllerType &&
		existing.ControlKeyID == request.ControlKeyID &&
		existing.AgentTaskID == request.AgentTaskID &&
		existing.AgentStepID == request.AgentStepID &&
		existing.ActionName == request.Descriptor.Name &&
		existing.ActionVersion == request.Descriptor.Version &&
		existing.ExecutionMode == request.Descriptor.ExecutionMode &&
		existing.InputHash == inputHash &&
		existing.ParentCommandID == request.ParentCommandID &&
		existing.RetryOfCommandID == request.RetryOfCommandID
}

func validItemStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case "queued", "running", "waiting_workflow", "succeeded", "failed", "cancelled", "skipped":
		return true
	default:
		return false
	}
}

func itemStatusTerminal(status string) bool {
	switch strings.TrimSpace(status) {
	case "succeeded", "failed", "cancelled", "skipped":
		return true
	default:
		return false
	}
}

func validItemTransition(from, to string) bool {
	if from == to {
		return !itemStatusTerminal(from)
	}
	switch from {
	case "queued":
		return to == "running" || to == "waiting_workflow" || itemStatusTerminal(to)
	case "running":
		return to == "waiting_workflow" || itemStatusTerminal(to)
	case "waiting_workflow":
		return itemStatusTerminal(to)
	default:
		return false
	}
}

func commandStatusEvent(status CommandStatus) string {
	switch status {
	case CommandRunning:
		return "project.control.command.running"
	case CommandWaitingWorkflow:
		return "project.control.command.waiting_workflow"
	case CommandWaitingInput:
		return "project.control.command.waiting_input"
	case CommandSucceeded:
		return "project.control.command.succeeded"
	case CommandPartialSucceeded:
		return "project.control.command.partial_succeeded"
	case CommandFailed:
		return "project.control.command.failed"
	case CommandCancelled:
		return "project.control.command.cancelled"
	default:
		return "project.control.command.progress"
	}
}

func timestampPointer(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time
	return &result
}

func intervalLiteral(duration time.Duration) string {
	return fmt.Sprintf("%d microseconds", duration.Microseconds())
}
