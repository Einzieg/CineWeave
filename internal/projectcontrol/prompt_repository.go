package projectcontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (r *Repository) CreatePrompt(ctx context.Context, request CreateCommandPrompt) (CommandPrompt, Command, error) {
	if strings.TrimSpace(request.PromptKind) == "" || strings.TrimSpace(request.Prompt) == "" {
		return CommandPrompt{}, Command{}, fmt.Errorf("prompt kind and text are required")
	}
	if !request.ExpiresAt.After(time.Now()) {
		return CommandPrompt{}, Command{}, fmt.Errorf("prompt expiration must be in the future")
	}
	options, err := canonicalArray(request.Options, 32768)
	if err != nil {
		return CommandPrompt{}, Command{}, fmt.Errorf("normalize prompt options: %w", err)
	}
	candidateRevisions, _, err := canonicalObject(request.CandidateRevisions, 32768)
	if err != nil {
		return CommandPrompt{}, Command{}, fmt.Errorf("normalize prompt candidate revisions: %w", err)
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return CommandPrompt{}, Command{}, err
	}
	defer tx.Rollback(ctx)
	current, err := getCommandTx(ctx, tx, request.CommandID, true)
	if err != nil {
		return CommandPrompt{}, Command{}, err
	}
	if current.Revision != request.ExpectedRevision {
		return CommandPrompt{}, Command{}, ErrRevisionConflict
	}
	if current.Status != CommandRunning && current.Status != CommandWaitingWorkflow {
		return CommandPrompt{}, Command{}, fmt.Errorf("command %s cannot request input while %s", current.ID, current.Status)
	}
	promptID := uuid.NewString()
	if _, err := tx.Exec(ctx, `
		INSERT INTO project_control_command_prompts(
			id, command_id, prompt_kind, prompt, options, status,
			expected_command_revision, candidate_revisions, expires_at
		)
		VALUES ($1, $2, $3, $4, $5, 'pending', $6, $7, $8)
	`, promptID, current.ID, strings.TrimSpace(request.PromptKind), strings.TrimSpace(request.Prompt),
		options, current.Revision+1, candidateRevisions, request.ExpiresAt); err != nil {
		return CommandPrompt{}, Command{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE project_control_commands
		SET status = 'waiting_input', lease_owner = NULL, lease_expires_at = NULL,
		    next_reconcile_at = $2, revision = revision + 1
		WHERE id = $1 AND revision = $3
	`, current.ID, request.ExpiresAt, current.Revision); err != nil {
		return CommandPrompt{}, Command{}, err
	}
	updated, err := getCommandTx(ctx, tx, current.ID, false)
	if err != nil {
		return CommandPrompt{}, Command{}, err
	}
	prompt, err := getCommandPromptTx(ctx, tx, promptID, false)
	if err != nil {
		return CommandPrompt{}, Command{}, err
	}
	if _, err := appendCommandEventTx(ctx, tx, updated, "project.control.command.waiting_input", map[string]any{
		"promptId":   prompt.ID,
		"promptKind": prompt.PromptKind,
	}); err != nil {
		return CommandPrompt{}, Command{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CommandPrompt{}, Command{}, err
	}
	return prompt, updated, nil
}

func (r *Repository) ResolvePrompt(ctx context.Context, request ResolveCommandPrompt) (CommandPrompt, Command, bool, error) {
	if strings.TrimSpace(request.ActorUserID) == "" || strings.TrimSpace(request.IdempotencyKey) == "" || len(request.IdempotencyKey) > 200 {
		return CommandPrompt{}, Command{}, false, fmt.Errorf("actor and valid idempotency key are required")
	}
	if request.ResumeStatus != CommandQueued && request.ResumeStatus != CommandRunning {
		return CommandPrompt{}, Command{}, false, fmt.Errorf("prompt resume status %q is invalid", request.ResumeStatus)
	}
	answer, _, err := canonicalObject(request.Answer, 32768)
	if err != nil {
		return CommandPrompt{}, Command{}, false, fmt.Errorf("normalize prompt answer: %w", err)
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return CommandPrompt{}, Command{}, false, err
	}
	defer tx.Rollback(ctx)
	current, err := getCommandTx(ctx, tx, request.CommandID, true)
	if err != nil {
		return CommandPrompt{}, Command{}, false, err
	}
	if current.ActorUserID != request.ActorUserID {
		return CommandPrompt{}, Command{}, false, fmt.Errorf("prompt actor does not own command")
	}
	prompt, err := getCommandPromptTx(ctx, tx, request.PromptID, true)
	if err != nil {
		return CommandPrompt{}, Command{}, false, err
	}
	if prompt.CommandID != current.ID {
		return CommandPrompt{}, Command{}, false, ErrPromptNotFound
	}
	if prompt.Status == "answered" {
		if prompt.AnswerIdempotencyKey != request.IdempotencyKey {
			return CommandPrompt{}, Command{}, false, ErrPromptAlreadyResolved
		}
		if err := tx.Commit(ctx); err != nil {
			return CommandPrompt{}, Command{}, false, err
		}
		return prompt, current, true, nil
	}
	if prompt.Status != "pending" {
		return CommandPrompt{}, Command{}, false, ErrPromptAlreadyResolved
	}
	if current.Status != CommandWaitingInput || current.Revision != request.ExpectedCommandRevision ||
		prompt.ExpectedCommandRevision != request.ExpectedCommandRevision {
		return CommandPrompt{}, Command{}, false, ErrRevisionConflict
	}
	if !prompt.ExpiresAt.After(time.Now()) {
		failed, expireErr := expirePromptTx(ctx, tx, current, prompt)
		if expireErr != nil {
			return CommandPrompt{}, Command{}, false, expireErr
		}
		if err := tx.Commit(ctx); err != nil {
			return CommandPrompt{}, Command{}, false, err
		}
		prompt.Status = "expired"
		return prompt, failed, false, ErrPromptExpired
	}
	if _, err := tx.Exec(ctx, `
		UPDATE project_control_command_prompts
		SET status = 'answered', answer = $2, answer_idempotency_key = $3,
		    answered_by_user_id = $4, answered_at = now()
		WHERE id = $1 AND status = 'pending'
	`, prompt.ID, answer, request.IdempotencyKey, request.ActorUserID); err != nil {
		return CommandPrompt{}, Command{}, false, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE project_control_commands
		SET status = $2, lease_owner = NULL, lease_expires_at = NULL,
		    next_reconcile_at = NULL, revision = revision + 1
		WHERE id = $1 AND revision = $3
	`, current.ID, request.ResumeStatus, current.Revision); err != nil {
		return CommandPrompt{}, Command{}, false, err
	}
	updated, err := getCommandTx(ctx, tx, current.ID, false)
	if err != nil {
		return CommandPrompt{}, Command{}, false, err
	}
	prompt, err = getCommandPromptTx(ctx, tx, prompt.ID, false)
	if err != nil {
		return CommandPrompt{}, Command{}, false, err
	}
	if _, err := appendCommandEventTx(ctx, tx, updated, "project.control.command.resumed", map[string]any{
		"promptId": prompt.ID,
	}); err != nil {
		return CommandPrompt{}, Command{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CommandPrompt{}, Command{}, false, err
	}
	return prompt, updated, false, nil
}

func (r *Repository) PendingPrompt(ctx context.Context, commandID string) (CommandPrompt, error) {
	prompt, err := scanCommandPrompt(r.db.QueryRow(ctx, commandPromptSelectSQL+`
		WHERE command_id = $1 AND status = 'pending'
	`, commandID))
	if errors.Is(err, pgx.ErrNoRows) {
		return CommandPrompt{}, ErrPromptNotFound
	}
	return prompt, err
}

func (r *Repository) ExpireNextPrompt(ctx context.Context) (Command, bool, error) {
	var commandID string
	err := r.db.QueryRow(ctx, `
		SELECT command_id::text
		FROM project_control_command_prompts
		WHERE status = 'pending' AND expires_at <= now()
		ORDER BY expires_at, id
		LIMIT 1
	`).Scan(&commandID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Command{}, false, nil
	}
	if err != nil {
		return Command{}, false, err
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Command{}, false, err
	}
	defer tx.Rollback(ctx)
	command, err := getCommandTx(ctx, tx, commandID, true)
	if err != nil {
		return Command{}, false, err
	}
	prompt, err := scanCommandPrompt(tx.QueryRow(ctx, commandPromptSelectSQL+`
		WHERE command_id = $1 AND status = 'pending' AND expires_at <= now()
		ORDER BY expires_at, id
		FOR UPDATE
		LIMIT 1
	`, commandID))
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return Command{}, false, err
		}
		return Command{}, false, nil
	}
	if err != nil {
		return Command{}, false, err
	}
	if command.Status != CommandWaitingInput {
		return Command{}, false, fmt.Errorf("pending prompt command %s has status %s", command.ID, command.Status)
	}
	failed, err := expirePromptTx(ctx, tx, command, prompt)
	if err != nil {
		return Command{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Command{}, false, err
	}
	return failed, true, nil
}

func expirePromptTx(ctx context.Context, tx pgx.Tx, command Command, prompt CommandPrompt) (Command, error) {
	if _, err := tx.Exec(ctx, `
		UPDATE project_control_command_prompts
		SET status = 'expired'
		WHERE id = $1 AND status = 'pending'
	`, prompt.ID); err != nil {
		return Command{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE project_control_commands
		SET status = 'failed', error_code = 'COMMAND_INPUT_EXPIRED',
		    error_message = '等待用户输入已超时', completed_at = now(),
		    lease_owner = NULL, lease_expires_at = NULL, next_reconcile_at = NULL,
		    revision = revision + 1
		WHERE id = $1 AND revision = $2
	`, command.ID, command.Revision); err != nil {
		return Command{}, err
	}
	updated, err := getCommandTx(ctx, tx, command.ID, false)
	if err != nil {
		return Command{}, err
	}
	if _, err := appendCommandEventTx(ctx, tx, updated, "project.control.command.failed", map[string]any{
		"promptId":  prompt.ID,
		"errorCode": "COMMAND_INPUT_EXPIRED",
	}); err != nil {
		return Command{}, err
	}
	return updated, nil
}

const commandPromptSelectSQL = `
	SELECT id::text, command_id::text, prompt_kind, prompt, options, status,
	       expected_command_revision, candidate_revisions, expires_at, answer,
	       COALESCE(answer_idempotency_key, ''), COALESCE(answered_by_user_id::text, ''),
	       created_at, answered_at
	FROM project_control_command_prompts
`

func scanCommandPrompt(row rowScanner) (CommandPrompt, error) {
	var prompt CommandPrompt
	var options, candidateRevisions, answer []byte
	var answeredAt pgtype.Timestamptz
	if err := row.Scan(&prompt.ID, &prompt.CommandID, &prompt.PromptKind, &prompt.Prompt,
		&options, &prompt.Status, &prompt.ExpectedCommandRevision, &candidateRevisions,
		&prompt.ExpiresAt, &answer, &prompt.AnswerIdempotencyKey,
		&prompt.AnsweredByUserID, &prompt.CreatedAt, &answeredAt); err != nil {
		return CommandPrompt{}, err
	}
	prompt.Options = cloneRawMessage(options)
	prompt.CandidateRevisions = cloneRawMessage(candidateRevisions)
	prompt.Answer = cloneRawMessage(answer)
	prompt.AnsweredAt = timestampPointer(answeredAt)
	return prompt, nil
}

func getCommandPromptTx(ctx context.Context, tx pgx.Tx, promptID string, lock bool) (CommandPrompt, error) {
	query := commandPromptSelectSQL + ` WHERE id = $1`
	if lock {
		query += ` FOR UPDATE`
	}
	prompt, err := scanCommandPrompt(tx.QueryRow(ctx, query, promptID))
	if errors.Is(err, pgx.ErrNoRows) {
		return CommandPrompt{}, ErrPromptNotFound
	}
	return prompt, err
}

func canonicalArray(raw json.RawMessage, maximumBytes int) (json.RawMessage, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`[]`)
	}
	var value []any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("value must be a JSON array: %w", err)
	}
	if value == nil {
		return nil, fmt.Errorf("value must be a JSON array")
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if len(canonical) > maximumBytes {
		return nil, fmt.Errorf("JSON array exceeds %d bytes", maximumBytes)
	}
	return canonical, nil
}
