package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Einzieg/cineweave/internal/events"
	"github.com/Einzieg/cineweave/internal/videoproduction"
	"github.com/jackc/pgx/v5"
	"go.temporal.io/sdk/temporal"
)

type NodeRunInput struct {
	OrganizationID    string
	ProjectID         string
	WorkflowRunID     string
	NodeKey           string
	NodeType          string
	Input             json.RawMessage
	AttemptGeneration int
}

// NodeExecution is the immutable capability captured when an Activity starts a
// node attempt. Business results must present the execution fence fields;
// production identity is copied from the workflow run and must never be
// resolved from the project's current generation at completion time. Loading
// either identity from mutable state would let a stale Activity write into a
// newer attempt or production generation.
type NodeExecution struct {
	NodeRunID                      string `json:"nodeRunId"`
	ExecutionToken                 string `json:"executionToken"`
	AttemptGeneration              int    `json:"attemptGeneration"`
	ProductionGenerationID         string `json:"productionGenerationId,omitempty"`
	VideoProductionBindingID       string `json:"videoProductionBindingId,omitempty"`
	VideoProductionBindingRevision int64  `json:"videoProductionBindingRevision,omitempty"`
}

func (execution NodeExecution) valid() bool {
	return strings.TrimSpace(execution.NodeRunID) != "" &&
		strings.TrimSpace(execution.ExecutionToken) != "" &&
		execution.AttemptGeneration > 0
}

type nodeRunContext struct {
	OrganizationID                 string
	ProjectID                      string
	WorkflowRunID                  string
	NodeKey                        string
	NodeStatus                     string
	WorkflowStatus                 string
	ExecutionToken                 string
	AttemptGeneration              int
	ProductionGenerationID         string
	VideoProductionBindingID       string
	VideoProductionBindingRevision int64
}

var workflowTerminalStatuses = map[string]string{
	"succeeded":         "workflow.run.completed",
	"partial_succeeded": "workflow.run.partial_succeeded",
	"failed":            "workflow.run.failed",
	"skipped":           "workflow.run.skipped",
}

const (
	CodeWorkflowResultDiscarded = "WORKFLOW_RESULT_DISCARDED"
	workflowWriteFenceMessage   = "workflow execution is no longer writable"
)

var workflowWriteFenceCause = errors.New(workflowWriteFenceMessage)
var ErrWorkflowWriteFenced = temporal.NewNonRetryableApplicationError(
	workflowWriteFenceMessage,
	CodeWorkflowResultDiscarded,
	workflowWriteFenceCause,
)

const workflowCancellationGracePeriod = 2 * time.Minute

func StartNodeRun(ctx context.Context, db txBeginner, input NodeRunInput) (NodeExecution, error) {
	if strings.TrimSpace(input.OrganizationID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.WorkflowRunID) == "" {
		return NodeExecution{}, fmt.Errorf("organizationId, projectId, and workflowRunId are required")
	}
	if strings.TrimSpace(input.NodeKey) == "" || strings.TrimSpace(input.NodeType) == "" {
		return NodeExecution{}, fmt.Errorf("nodeKey and nodeType are required")
	}
	nodeInput := input.Input
	if len(nodeInput) == 0 {
		nodeInput = json.RawMessage(`{}`)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return NodeExecution{}, err
	}
	defer tx.Rollback(ctx)
	// Provider requests acquire project foreign-key locks before workflow/node
	// locks. Keep the same global order here to avoid deadlocks when a batch
	// starts new nodes while earlier provider calls are being recorded.
	productionContext, err := videoproduction.LoadWritableContextTx(ctx, tx, input.ProjectID, true)
	if err != nil {
		if _, ok := videoproduction.AsError(err); ok || errors.Is(err, pgx.ErrNoRows) {
			return NodeExecution{}, ErrWorkflowWriteFenced
		}
		return NodeExecution{}, err
	}

	var workflowStatus, organizationID, projectID string
	var workflowGeneration int
	var productionGenerationID, bindingID string
	var bindingRevision int64
	if err := tx.QueryRow(ctx, `
		SELECT status, attempt_generation, organization_id::text, project_id::text,
		       production_generation_id::text, video_production_binding_id::text,
		       video_production_binding_revision
		FROM workflow_runs
		WHERE id = $1
		FOR UPDATE
	`, input.WorkflowRunID).Scan(
		&workflowStatus, &workflowGeneration, &organizationID, &projectID,
		&productionGenerationID, &bindingID, &bindingRevision,
	); err != nil {
		return NodeExecution{}, err
	}
	if organizationID != input.OrganizationID || projectID != input.ProjectID {
		return NodeExecution{}, ErrWorkflowWriteFenced
	}
	if workflowStatus != "pending" && workflowStatus != "queued" && workflowStatus != "running" {
		return NodeExecution{}, ErrWorkflowWriteFenced
	}
	if input.AttemptGeneration > 0 && input.AttemptGeneration != workflowGeneration {
		return NodeExecution{}, ErrWorkflowWriteFenced
	}
	input.AttemptGeneration = workflowGeneration
	allowLocked, err := workflowMayWriteLockedProductionGeneration(
		ctx, tx, input.WorkflowRunID, projectID, productionGenerationID, bindingID, bindingRevision,
	)
	if err != nil {
		return NodeExecution{}, err
	}
	if (productionContext.Locked && !allowLocked) ||
		productionContext.Generation.ID != productionGenerationID ||
		productionContext.Binding.ID != bindingID ||
		productionContext.Binding.Revision != bindingRevision {
		return NodeExecution{}, ErrWorkflowWriteFenced
	}
	var workflowRevision int64
	if err := tx.QueryRow(ctx, `
		UPDATE workflow_runs
		SET status = 'running', started_at = COALESCE(started_at, now()),
		    revision = revision + 1, updated_at = now()
		WHERE id = $1
		  AND status IN ('pending', 'queued', 'running')
		RETURNING revision
	`, input.WorkflowRunID).Scan(&workflowRevision); err != nil {
		return NodeExecution{}, err
	}
	var execution NodeExecution
	var nodeRevision int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO workflow_node_runs(
			organization_id, project_id, workflow_run_id, node_key, node_type,
			status, input, started_at, attempt_generation, production_generation_id
		)
		VALUES ($1, $2, $3, $4, $5, 'running', $6, now(), $7, $8)
		ON CONFLICT (workflow_run_id, node_key) DO UPDATE SET
			status = 'running',
			input = EXCLUDED.input,
			execution_token = gen_random_uuid(),
			retry_count = workflow_node_runs.retry_count + CASE WHEN workflow_node_runs.status = 'queued' THEN 0 ELSE 1 END,
			error_code = NULL,
			error_message = NULL,
			started_at = now(),
			completed_at = NULL,
			revision = workflow_node_runs.revision + 1,
			updated_at = now()
		WHERE workflow_node_runs.status IN ('pending', 'queued', 'running', 'failed')
		  AND workflow_node_runs.attempt_generation = EXCLUDED.attempt_generation
		RETURNING id::text, execution_token::text, attempt_generation, revision
	`, input.OrganizationID, input.ProjectID, input.WorkflowRunID, input.NodeKey, input.NodeType,
		nodeInput, input.AttemptGeneration, productionGenerationID).Scan(
		&execution.NodeRunID, &execution.ExecutionToken, &execution.AttemptGeneration, &nodeRevision,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return NodeExecution{}, ErrWorkflowWriteFenced
		}
		return NodeExecution{}, err
	}
	execution.ProductionGenerationID = productionGenerationID
	execution.VideoProductionBindingID = bindingID
	execution.VideoProductionBindingRevision = bindingRevision
	if err := insertEvent(ctx, tx, input.OrganizationID, input.ProjectID, "workflow.node.started", "workflow_node_run", execution.NodeRunID, mustJSON(map[string]any{
		"workflowRunId":     input.WorkflowRunID,
		"nodeKey":           input.NodeKey,
		"revision":          nodeRevision,
		"workflowRevision":  workflowRevision,
		"attemptGeneration": input.AttemptGeneration,
	})); err != nil {
		return NodeExecution{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return NodeExecution{}, err
	}
	return execution, nil
}

func CompleteNodeRun(ctx context.Context, db txBeginner, execution NodeExecution, output json.RawMessage) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	applied, err := completeNodeRunTx(ctx, tx, execution, output)
	if err != nil {
		return err
	}
	if !applied {
		if err := insertWorkflowResultDiscardedTx(ctx, tx, execution, "node completion arrived after the execution fence closed"); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

func ProgressNodeRun(ctx context.Context, db txBeginner, execution NodeExecution, output json.RawMessage) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := progressNodeRunTx(ctx, tx, execution, output); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func FailNodeRun(ctx context.Context, db txBeginner, execution NodeExecution, code, message string) error {
	return FailNodeRunWithOutput(ctx, db, execution, code, message, json.RawMessage(`{}`))
}

func FailNodeRunWithOutput(ctx context.Context, db txBeginner, execution NodeExecution, code, message string, output json.RawMessage) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := failNodeRunTx(ctx, tx, execution, code, message, output); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func TransitionWorkflowRun(
	ctx context.Context,
	db txBeginner,
	workflowRunID, status, code, message string,
	output json.RawMessage,
) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, _, err := transitionWorkflowRunTx(ctx, tx, workflowRunID, status, code, message, output); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func transitionWorkflowRunTx(
	ctx context.Context,
	tx pgx.Tx,
	workflowRunID, status, code, message string,
	output json.RawMessage,
) (nodeRunContext, bool, error) {
	eventType, ok := workflowTerminalStatuses[status]
	if !ok {
		return nodeRunContext{}, false, fmt.Errorf("unsupported workflow terminal status %q", status)
	}
	if len(output) == 0 {
		output = json.RawMessage(`{}`)
	}
	runCtx, err := lockWorkflowRunContext(ctx, tx, workflowRunID)
	if err != nil {
		return nodeRunContext{}, false, err
	}
	var revision int64
	err = tx.QueryRow(ctx, `
		UPDATE workflow_runs
		SET status = $2,
		    output = $3,
		    error_code = NULLIF($4, ''),
		    error_message = NULLIF($5, ''),
		    completed_at = COALESCE(completed_at, now()),
		    terminalized_at = COALESCE(terminalized_at, now()),
		    settled_at = COALESCE(settled_at, now()),
		    revision = revision + 1,
		    updated_at = now()
		WHERE id = $1
		  AND status IN ('pending', 'queued', 'running', 'waiting_review')
		RETURNING revision
	`, workflowRunID, status, output, code, message).Scan(&revision)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return runCtx, false, nil
		}
		return nodeRunContext{}, false, err
	}
	if _, err := settleTerminalWorkflowNodesTx(ctx, tx, runCtx, workflowRunID, revision, status, code, message); err != nil {
		return nodeRunContext{}, false, err
	}
	payload := workflowRunEventPayload(workflowRunID, status, revision, output, code, message)
	if err := insertEvent(ctx, tx, runCtx.OrganizationID, runCtx.ProjectID, eventType, "workflow_run", workflowRunID, payload); err != nil {
		return nodeRunContext{}, false, err
	}
	return runCtx, true, nil
}

type unsettledWorkflowNode struct {
	ID       string
	NodeKey  string
	Revision int64
}

func settleTerminalWorkflowNodesTx(
	ctx context.Context,
	tx pgx.Tx,
	runCtx nodeRunContext,
	workflowRunID string,
	workflowRevision int64,
	workflowStatus string,
	code string,
	message string,
) (int, error) {
	code, message = unsettledTerminalNodeError(workflowStatus, code, message)
	rows, err := tx.Query(ctx, `
		UPDATE workflow_node_runs
		SET status = 'failed',
		    error_code = $2,
		    error_message = $3,
		    completed_at = COALESCE(completed_at, now()),
		    revision = revision + 1,
		    updated_at = now()
		WHERE workflow_run_id = $1
		  AND status IN ('pending', 'queued', 'running', 'waiting_review')
		RETURNING id::text, node_key, revision
	`, workflowRunID, code, message)
	if err != nil {
		return 0, err
	}
	nodes := make([]unsettledWorkflowNode, 0)
	for rows.Next() {
		var node unsettledWorkflowNode
		if err := rows.Scan(&node.ID, &node.NodeKey, &node.Revision); err != nil {
			rows.Close()
			return 0, err
		}
		nodes = append(nodes, node)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, node := range nodes {
		if err := insertEvent(ctx, tx, runCtx.OrganizationID, runCtx.ProjectID, "workflow.node.failed", "workflow_node_run", node.ID, mustJSON(map[string]any{
			"workflowRunId":    workflowRunID,
			"nodeRunId":        node.ID,
			"nodeKey":          node.NodeKey,
			"status":           "failed",
			"code":             code,
			"message":          message,
			"revision":         node.Revision,
			"workflowRevision": workflowRevision,
		})); err != nil {
			return 0, err
		}
	}
	return len(nodes), nil
}

func unsettledTerminalNodeError(workflowStatus, code, message string) (string, string) {
	code = strings.TrimSpace(code)
	message = strings.TrimSpace(message)
	if workflowStatus == "failed" {
		if code == "" {
			code = "WORKFLOW_FAILED"
		}
		if message == "" {
			message = "工作流失败时节点尚未正常终结"
		}
		return code, message
	}
	if code == "" {
		code = "WORKFLOW_NODE_UNSETTLED"
		if workflowStatus == "partial_succeeded" {
			code = "WORKFLOW_PARTIAL_SUCCEEDED"
		}
	}
	if message == "" {
		message = "工作流已进入终态，未完成节点已由状态协调器终结"
	}
	return code, message
}

// ReconcileTerminalWorkflowNodes repairs unfinished nodes left behind after a
// workflow reached a terminal state, including partial-success workflows.
func ReconcileTerminalWorkflowNodes(ctx context.Context, db txBeginner) (int, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	type terminalWorkflow struct {
		Context  nodeRunContext
		Revision int64
		Status   string
		Code     string
		Message  string
	}
	rows, err := tx.Query(ctx, `
		SELECT run.id::text, run.organization_id::text, run.project_id::text,
		       run.revision, run.status, COALESCE(run.error_code, ''), COALESCE(run.error_message, '')
		FROM workflow_runs run
		WHERE run.status IN ('succeeded', 'partial_succeeded', 'failed', 'skipped')
		  AND EXISTS (
		    SELECT 1 FROM workflow_node_runs node
		    WHERE node.workflow_run_id = run.id
		      AND node.status IN ('pending', 'queued', 'running', 'waiting_review')
		  )
		FOR UPDATE OF run SKIP LOCKED
	`)
	if err != nil {
		return 0, err
	}
	runs := make([]terminalWorkflow, 0)
	for rows.Next() {
		var run terminalWorkflow
		if err := rows.Scan(
			&run.Context.WorkflowRunID,
			&run.Context.OrganizationID,
			&run.Context.ProjectID,
			&run.Revision,
			&run.Status,
			&run.Code,
			&run.Message,
		); err != nil {
			rows.Close()
			return 0, err
		}
		run.Context.WorkflowStatus = run.Status
		runs = append(runs, run)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	settled := 0
	for _, run := range runs {
		count, err := settleTerminalWorkflowNodesTx(
			ctx,
			tx,
			run.Context,
			run.Context.WorkflowRunID,
			run.Revision,
			run.Status,
			run.Code,
			run.Message,
		)
		if err != nil {
			return 0, err
		}
		settled += count
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return settled, nil
}

func workflowRunEventPayload(workflowRunID, status string, revision int64, output json.RawMessage, code, message string) json.RawMessage {
	payload := map[string]any{}
	if len(output) > 0 {
		_ = json.Unmarshal(output, &payload)
	}
	payload["workflowRunId"] = workflowRunID
	payload["status"] = status
	payload["revision"] = revision
	if code != "" {
		payload["code"] = code
	}
	if message != "" {
		payload["message"] = message
	}
	return mustJSON(payload)
}

func completeNodeRunTx(ctx context.Context, tx pgx.Tx, execution NodeExecution, output json.RawMessage) (bool, error) {
	return transitionNodeRunTx(ctx, tx, execution, "succeeded", "", "", output, "workflow.node.completed")
}

func progressNodeRunTx(ctx context.Context, tx pgx.Tx, execution NodeExecution, output json.RawMessage) (bool, error) {
	return transitionNodeRunTx(ctx, tx, execution, "running", "", "", output, "workflow.node.progress")
}

func failNodeRunTx(ctx context.Context, tx pgx.Tx, execution NodeExecution, code, message string, output json.RawMessage) (bool, error) {
	return transitionNodeRunTx(ctx, tx, execution, "failed", code, message, output, "workflow.node.failed")
}

func transitionNodeRunTx(
	ctx context.Context,
	tx pgx.Tx,
	execution NodeExecution,
	status, code, message string,
	output json.RawMessage,
	eventType string,
) (bool, error) {
	if !execution.valid() {
		return false, ErrWorkflowWriteFenced
	}
	if len(output) == 0 {
		output = json.RawMessage(`{}`)
	}
	runCtx, err := lockNodeRunContext(ctx, tx, execution)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	if !runCtx.writable() {
		return false, nil
	}
	if status == "succeeded" || status == "running" {
		writable, err := nodeProductionGenerationWritable(ctx, tx, runCtx)
		if err != nil {
			return false, err
		}
		if !writable {
			return false, nil
		}
	}
	var nodeRevision int64
	if err := tx.QueryRow(ctx, `
		UPDATE workflow_node_runs
		SET status = $2,
		    error_code = NULLIF($3, ''),
		    error_message = NULLIF($4, ''),
		    output = $5,
		    completed_at = CASE WHEN $2 = 'running' THEN NULL ELSE now() END,
		    revision = revision + 1,
		    updated_at = now()
		WHERE id = $1
		  AND status = 'running'
		  AND execution_token = $6
		  AND attempt_generation = $7
		RETURNING revision
	`, execution.NodeRunID, status, code, message, output, execution.ExecutionToken, execution.AttemptGeneration).Scan(&nodeRevision); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	workflowRevision, err := updateWorkflowProgressTx(ctx, tx, runCtx.WorkflowRunID)
	if err != nil {
		return false, err
	}
	payload := map[string]any{
		"workflowRunId":    runCtx.WorkflowRunID,
		"nodeKey":          runCtx.NodeKey,
		"output":           json.RawMessage(output),
		"revision":         nodeRevision,
		"workflowRevision": workflowRevision,
	}
	if status == "failed" {
		payload["code"] = code
		payload["message"] = message
	}
	if err := insertEvent(ctx, tx, runCtx.OrganizationID, runCtx.ProjectID, eventType, "workflow_node_run", execution.NodeRunID, mustJSON(payload)); err != nil {
		return false, err
	}
	return true, nil
}

func CancelNodeRun(ctx context.Context, db txBeginner, nodeRunID string, output json.RawMessage, reason string) error {
	if len(output) == 0 {
		output = json.RawMessage(`{}`)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	runCtx, err := lockNodeRunContextByID(ctx, tx, nodeRunID)
	if err != nil {
		return err
	}
	var nodeRevision int64
	if err := tx.QueryRow(ctx, `
		UPDATE workflow_node_runs
		SET status = 'cancelled',
		    output = $2,
		    error_code = 'USER_CANCELLED',
		    error_message = $3,
		    completed_at = now(),
		    revision = revision + 1,
		    updated_at = now()
		WHERE id = $1
		  AND status IN ('pending', 'queued', 'running', 'waiting_review')
		RETURNING revision
	`, nodeRunID, output, nullableCancelReason(reason)).Scan(&nodeRevision); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return tx.Commit(ctx)
		}
		return err
	}
	workflowRevision, err := updateWorkflowProgressTx(ctx, tx, runCtx.WorkflowRunID)
	if err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, runCtx.OrganizationID, runCtx.ProjectID, "workflow.node.cancelled", "workflow_node_run", nodeRunID, mustJSON(map[string]any{
		"workflowRunId":    runCtx.WorkflowRunID,
		"nodeRunId":        nodeRunID,
		"nodeKey":          runCtx.NodeKey,
		"reason":           reason,
		"status":           "cancelled",
		"output":           json.RawMessage(output),
		"revision":         nodeRevision,
		"workflowRevision": workflowRevision,
	})); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func MarkWorkflowCancelling(ctx context.Context, db txBeginner, workflowRunID, reason string) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	runCtx, err := lockWorkflowRunContext(ctx, tx, workflowRunID)
	if err != nil {
		return err
	}
	var revision int64
	var deadline time.Time
	if err := tx.QueryRow(ctx, `
		UPDATE workflow_runs
		SET status = 'cancelling',
		    error_code = 'USER_CANCEL_REQUESTED',
		    error_message = $2,
		    cancellation_requested_at = COALESCE(cancellation_requested_at, now()),
		    cancellation_deadline_at = COALESCE(cancellation_deadline_at, now() + $3::interval),
		    revision = revision + 1,
		    updated_at = now()
		WHERE id = $1
		  AND status IN ('pending', 'queued', 'running', 'cancelling')
		RETURNING revision, cancellation_deadline_at
	`, workflowRunID, nullableCancelReason(reason), workflowCancellationGracePeriod.String()).Scan(&revision, &deadline); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return tx.Commit(ctx)
		}
		return err
	}
	if err := insertEvent(ctx, tx, runCtx.OrganizationID, runCtx.ProjectID, "workflow.run.cancelling", "workflow_run", workflowRunID, mustJSON(map[string]any{
		"workflowRunId": workflowRunID,
		"reason":        reason,
		"status":        "cancelling",
		"revision":      revision,
		"deadlineAt":    deadline.UTC().Format(time.RFC3339Nano),
	})); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func cancelWorkflowRunTx(
	ctx context.Context,
	tx pgx.Tx,
	workflowRunID string,
	output json.RawMessage,
	reason, errorCode string,
) (nodeRunContext, bool, error) {
	if len(output) == 0 {
		output = json.RawMessage(`{}`)
	}
	if strings.TrimSpace(errorCode) == "" {
		errorCode = "USER_CANCELLED"
	}
	runCtx, err := lockWorkflowRunContext(ctx, tx, workflowRunID)
	if err != nil {
		return nodeRunContext{}, false, err
	}
	var workflowRevision int64
	err = tx.QueryRow(ctx, `
		UPDATE workflow_runs
		SET status = 'cancelled',
		    output = $2,
		    error_code = $3,
		    error_message = $4,
		    completed_at = COALESCE(completed_at, now()),
		    cancelled_at = COALESCE(cancelled_at, now()),
		    terminalized_at = COALESCE(terminalized_at, now()),
		    settled_at = COALESCE(settled_at, now()),
		    revision = revision + 1,
		    updated_at = now()
		WHERE id = $1
		  AND status IN ('pending', 'queued', 'running', 'cancelling', 'waiting_review')
		RETURNING revision
	`, workflowRunID, output, errorCode, nullableCancelReason(reason)).Scan(&workflowRevision)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return runCtx, false, nil
		}
		return nodeRunContext{}, false, err
	}
	rows, err := tx.Query(ctx, `
		UPDATE workflow_node_runs
		SET status = 'cancelled',
		    error_code = $2,
		    error_message = $3,
		    completed_at = COALESCE(completed_at, now()),
		    revision = revision + 1,
		    updated_at = now()
		WHERE workflow_run_id = $1
		  AND status IN ('pending', 'queued', 'running', 'waiting_review')
		RETURNING id::text, node_key, revision
	`, workflowRunID, errorCode, nullableCancelReason(reason))
	if err != nil {
		return nodeRunContext{}, false, err
	}
	type cancelledNode struct {
		ID       string
		NodeKey  string
		Revision int64
	}
	cancelledNodes := make([]cancelledNode, 0)
	for rows.Next() {
		var node cancelledNode
		if err := rows.Scan(&node.ID, &node.NodeKey, &node.Revision); err != nil {
			rows.Close()
			return nodeRunContext{}, false, err
		}
		cancelledNodes = append(cancelledNodes, node)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nodeRunContext{}, false, err
	}
	for _, node := range cancelledNodes {
		if err := insertEvent(ctx, tx, runCtx.OrganizationID, runCtx.ProjectID, "workflow.node.cancelled", "workflow_node_run", node.ID, mustJSON(map[string]any{
			"workflowRunId": workflowRunID,
			"nodeRunId":     node.ID,
			"nodeKey":       node.NodeKey,
			"reason":        reason,
			"status":        "cancelled",
			"revision":      node.Revision,
		})); err != nil {
			return nodeRunContext{}, false, err
		}
	}
	payload := workflowRunEventPayload(workflowRunID, "cancelled", workflowRevision, output, errorCode, reason)
	if err := insertEvent(ctx, tx, runCtx.OrganizationID, runCtx.ProjectID, "workflow.run.cancelled", "workflow_run", workflowRunID, payload); err != nil {
		return nodeRunContext{}, false, err
	}
	return runCtx, true, nil
}

func CancelWorkflowRun(ctx context.Context, db txBeginner, workflowRunID string, output json.RawMessage, reason string) error {
	if len(output) == 0 {
		output = json.RawMessage(`{}`)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	runCtx, _, err := cancelWorkflowRunTx(ctx, tx, workflowRunID, output, reason, "USER_CANCELLED")
	if err != nil {
		return err
	}
	if err := cancelEpisodeVideoProductionCheckpointsForWorkflowTx(ctx, tx, workflowRunID, reason); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE storyboard_shots
		SET image_prompt_status = CASE
		      WHEN image_prompt_workflow_run_id = $1 AND image_prompt_status IN ('queued', 'running') THEN 'failed'
		      ELSE image_prompt_status
		    END,
		    image_prompt_error_code = CASE
		      WHEN image_prompt_workflow_run_id = $1 AND image_prompt_status IN ('queued', 'running') THEN 'USER_CANCELLED'
		      ELSE image_prompt_error_code
		    END,
		    image_prompt_error_message = CASE
		      WHEN image_prompt_workflow_run_id = $1 AND image_prompt_status IN ('queued', 'running') THEN COALESCE(NULLIF($2, ''), '关联工作流已取消，图片提示词未完成')
		      ELSE image_prompt_error_message
		    END,
		    image_prompt_updated_at = CASE
		      WHEN image_prompt_workflow_run_id = $1 AND image_prompt_status IN ('queued', 'running') THEN now()
		      ELSE image_prompt_updated_at
		    END,
		    image_status = CASE
		      WHEN image_workflow_run_id = $1 AND image_status IN ('queued', 'running') THEN 'failed'
		      ELSE image_status
		    END,
		    image_error_code = CASE
		      WHEN image_workflow_run_id = $1 AND image_status IN ('queued', 'running') THEN 'USER_CANCELLED'
		      ELSE image_error_code
		    END,
		    image_error_message = CASE
		      WHEN image_workflow_run_id = $1 AND image_status IN ('queued', 'running') THEN COALESCE(NULLIF($2, ''), '关联工作流已取消，图片生成未完成')
		      ELSE image_error_message
		    END,
		    image_completed_at = CASE
		      WHEN image_workflow_run_id = $1 AND image_status IN ('queued', 'running') THEN now()
		      ELSE image_completed_at
		    END,
		    video_prompt_status = CASE
		      WHEN video_prompt_workflow_run_id = $1 AND video_prompt_status IN ('queued', 'running') THEN 'failed'
		      ELSE video_prompt_status
		    END,
		    video_prompt_error_code = CASE
		      WHEN video_prompt_workflow_run_id = $1 AND video_prompt_status IN ('queued', 'running') THEN 'USER_CANCELLED'
		      ELSE video_prompt_error_code
		    END,
		    video_prompt_error_message = CASE
		      WHEN video_prompt_workflow_run_id = $1 AND video_prompt_status IN ('queued', 'running') THEN COALESCE(NULLIF($2, ''), '关联工作流已取消，视频提示词未完成')
		      ELSE video_prompt_error_message
		    END,
		    video_prompt_updated_at = CASE
		      WHEN video_prompt_workflow_run_id = $1 AND video_prompt_status IN ('queued', 'running') THEN now()
		      ELSE video_prompt_updated_at
		    END,
		    video_status = CASE
		      WHEN video_workflow_run_id = $1 AND video_status IN ('queued', 'running') THEN 'failed'
		      ELSE video_status
		    END,
		    video_error_code = CASE
		      WHEN video_workflow_run_id = $1 AND video_status IN ('queued', 'running') THEN 'USER_CANCELLED'
		      ELSE video_error_code
		    END,
		    video_error_message = CASE
		      WHEN video_workflow_run_id = $1 AND video_status IN ('queued', 'running') THEN COALESCE(NULLIF($2, ''), '关联工作流已取消，视频生成未完成')
		      ELSE video_error_message
		    END,
		    video_completed_at = CASE
		      WHEN video_workflow_run_id = $1 AND video_status IN ('queued', 'running') THEN now()
		      ELSE video_completed_at
		    END,
		    status = CASE
		      WHEN video_workflow_run_id = $1 AND video_status IN ('queued', 'running') THEN 'video_failed'
		      WHEN image_workflow_run_id = $1 AND image_status IN ('queued', 'running') THEN 'image_failed'
		      ELSE status
		    END,
		    updated_at = now()
		WHERE project_id = $3
		  AND deleted_at IS NULL
		  AND (
		    (image_prompt_workflow_run_id = $1 AND image_prompt_status IN ('queued', 'running'))
		    OR (image_workflow_run_id = $1 AND image_status IN ('queued', 'running'))
		    OR (video_prompt_workflow_run_id = $1 AND video_prompt_status IN ('queued', 'running'))
		    OR (video_workflow_run_id = $1 AND video_status IN ('queued', 'running'))
		  )
	`, workflowRunID, strings.TrimSpace(reason), runCtx.ProjectID); err != nil {
		return err
	}
	if err := restoreApprovedVideoPromptStatesForWorkflowTx(ctx, tx, runCtx.ProjectID, workflowRunID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func insertEvent(ctx context.Context, tx pgx.Tx, organizationID, projectID, eventType, aggregateType, aggregateID string, payload json.RawMessage) error {
	return events.AppendTx(ctx, tx, organizationID, projectID, eventType, aggregateType, aggregateID, payload)
}

func updateWorkflowProgressTx(ctx context.Context, tx pgx.Tx, workflowRunID string) (int64, error) {
	var revision int64
	err := tx.QueryRow(ctx, `
		WITH run AS (
			SELECT id, project_id, workflow_type
			FROM workflow_runs
			WHERE id = $1
		), node_counts AS (
			SELECT
				count(*) FILTER (WHERE status IN ('succeeded', 'skipped'))::integer AS completed,
				count(*) FILTER (WHERE status = 'failed')::integer AS failed
			FROM workflow_node_runs
			WHERE workflow_run_id = $1
			  AND node_type NOT LIKE 'workflow.%'
		), shot_counts AS (
			SELECT
				count(*) FILTER (WHERE
					(run.workflow_type = 'batch_generate_shot_image_prompts' AND s.image_prompt_workflow_run_id = run.id AND s.image_prompt_status = 'succeeded') OR
					(run.workflow_type = 'batch_generate_shot_images' AND s.image_workflow_run_id = run.id AND s.image_status = 'succeeded') OR
					(run.workflow_type = 'batch_generate_shot_video_prompts' AND s.video_prompt_workflow_run_id = run.id AND s.video_prompt_status = 'succeeded') OR
					(run.workflow_type = 'batch_generate_shot_videos' AND s.video_workflow_run_id = run.id AND s.video_status = 'succeeded')
				)::integer AS completed,
				count(*) FILTER (WHERE
					(run.workflow_type = 'batch_generate_shot_image_prompts' AND s.image_prompt_workflow_run_id = run.id AND s.image_prompt_status = 'failed') OR
					(run.workflow_type = 'batch_generate_shot_images' AND s.image_workflow_run_id = run.id AND s.image_status = 'failed') OR
					(run.workflow_type = 'batch_generate_shot_video_prompts' AND s.video_prompt_workflow_run_id = run.id AND s.video_prompt_status = 'failed') OR
					(run.workflow_type = 'batch_generate_shot_videos' AND s.video_workflow_run_id = run.id AND s.video_status = 'failed')
				)::integer AS failed
			FROM run
			LEFT JOIN storyboard_shots s
			  ON s.project_id = run.project_id
			 AND s.deleted_at IS NULL
		), counts AS (
			SELECT
				CASE WHEN run.workflow_type IN (
					'batch_generate_shot_image_prompts',
					'batch_generate_shot_images',
					'batch_generate_shot_video_prompts',
					'batch_generate_shot_videos'
				) THEN shot_counts.completed ELSE node_counts.completed END AS completed,
				CASE WHEN run.workflow_type IN (
					'batch_generate_shot_image_prompts',
					'batch_generate_shot_images',
					'batch_generate_shot_video_prompts',
					'batch_generate_shot_videos'
				) THEN shot_counts.failed ELSE node_counts.failed END AS failed
			FROM run, node_counts, shot_counts
		)
		UPDATE workflow_runs wr
		SET completed_items = CASE
		      WHEN wr.total_items > 0 THEN LEAST(wr.total_items, counts.completed)
		      ELSE wr.completed_items
		    END,
		    failed_items = CASE
		      WHEN wr.total_items > 0 THEN LEAST(
		        GREATEST(wr.total_items - LEAST(wr.total_items, counts.completed), 0),
		        counts.failed
		      )
		      ELSE wr.failed_items
		    END,
		    revision = wr.revision + 1,
		    updated_at = now()
		FROM counts
		WHERE wr.id = $1
		  AND wr.status IN ('pending', 'queued', 'running', 'cancelling')
		RETURNING wr.revision
	`, workflowRunID).Scan(&revision)
	return revision, err
}

func lockNodeRunContext(ctx context.Context, tx pgx.Tx, execution NodeExecution) (nodeRunContext, error) {
	if !execution.valid() {
		return nodeRunContext{}, ErrWorkflowWriteFenced
	}
	return lockNodeRunContextQuery(ctx, tx, execution.NodeRunID, execution.ExecutionToken, execution.AttemptGeneration, true)
}

func lockNodeRunContextByID(ctx context.Context, tx pgx.Tx, nodeRunID string) (nodeRunContext, error) {
	return lockNodeRunContextQuery(ctx, tx, nodeRunID, "", 0, false)
}

func lockNodeRunContextQuery(ctx context.Context, tx pgx.Tx, nodeRunID, executionToken string, attemptGeneration int, strict bool) (nodeRunContext, error) {
	var projectID string
	if err := tx.QueryRow(ctx, `
		SELECT project_id::text
		FROM workflow_node_runs
		WHERE id = $1
		  AND (NOT $4 OR (execution_token = NULLIF($2, '')::uuid AND attempt_generation = $3))
	`, nodeRunID, executionToken, attemptGeneration, strict).Scan(&projectID); err != nil {
		return nodeRunContext{}, err
	}
	if err := lockProjectRow(ctx, tx, projectID); err != nil {
		return nodeRunContext{}, err
	}

	var runCtx nodeRunContext
	err := tx.QueryRow(ctx, `
		SELECT node.organization_id, node.project_id, node.workflow_run_id, node.node_key,
		       node.status, run.status, node.execution_token::text, node.attempt_generation,
		       run.production_generation_id::text, run.video_production_binding_id::text,
		       run.video_production_binding_revision
		FROM workflow_runs run
		JOIN workflow_node_runs node ON node.workflow_run_id = run.id
		WHERE node.id = $1
		  AND node.production_generation_id = run.production_generation_id
		  AND (NOT $4 OR (node.execution_token = NULLIF($2, '')::uuid AND node.attempt_generation = $3))
		FOR UPDATE OF run, node
	`, nodeRunID, executionToken, attemptGeneration, strict).Scan(
		&runCtx.OrganizationID, &runCtx.ProjectID, &runCtx.WorkflowRunID, &runCtx.NodeKey,
		&runCtx.NodeStatus, &runCtx.WorkflowStatus, &runCtx.ExecutionToken, &runCtx.AttemptGeneration,
		&runCtx.ProductionGenerationID, &runCtx.VideoProductionBindingID, &runCtx.VideoProductionBindingRevision,
	)
	return runCtx, err
}

func (runCtx nodeRunContext) writable() bool {
	return runCtx.WorkflowStatus == "running" && runCtx.NodeStatus == "running"
}

func lockNodeBusinessWrite(ctx context.Context, tx pgx.Tx, workflowRunID string, execution NodeExecution) (nodeRunContext, error) {
	runCtx, err := lockNodeRunContext(ctx, tx, execution)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nodeRunContext{}, ErrWorkflowWriteFenced
		}
		return nodeRunContext{}, err
	}
	if runCtx.WorkflowRunID != workflowRunID || !runCtx.writable() {
		return nodeRunContext{}, ErrWorkflowWriteFenced
	}
	writable, err := nodeProductionGenerationWritable(ctx, tx, runCtx)
	if err != nil {
		return nodeRunContext{}, err
	}
	if !writable {
		return nodeRunContext{}, ErrWorkflowWriteFenced
	}
	return runCtx, nil
}

func lockWorkflowBusinessWrite(ctx context.Context, tx pgx.Tx, workflowRunID string) (nodeRunContext, error) {
	runCtx, err := lockWorkflowRunContext(ctx, tx, workflowRunID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nodeRunContext{}, ErrWorkflowWriteFenced
		}
		return nodeRunContext{}, err
	}
	if runCtx.WorkflowStatus != "pending" && runCtx.WorkflowStatus != "queued" && runCtx.WorkflowStatus != "running" && runCtx.WorkflowStatus != "waiting_review" {
		return nodeRunContext{}, ErrWorkflowWriteFenced
	}
	writable, err := nodeProductionGenerationWritable(ctx, tx, runCtx)
	if err != nil {
		return nodeRunContext{}, err
	}
	if !writable {
		return nodeRunContext{}, ErrWorkflowWriteFenced
	}
	return runCtx, nil
}

func nodeProductionGenerationWritable(ctx context.Context, tx pgx.Tx, runCtx nodeRunContext) (bool, error) {
	allowLocked, err := workflowMayWriteLockedProductionGeneration(
		ctx, tx, runCtx.WorkflowRunID, runCtx.ProjectID, runCtx.ProductionGenerationID,
		runCtx.VideoProductionBindingID, runCtx.VideoProductionBindingRevision,
	)
	if err != nil {
		return false, err
	}
	_, err = videoproduction.AssertWritableTx(
		ctx,
		tx,
		runCtx.ProjectID,
		runCtx.ProductionGenerationID,
		runCtx.VideoProductionBindingID,
		runCtx.VideoProductionBindingRevision,
		allowLocked,
	)
	if err != nil {
		if _, ok := videoproduction.AsError(err); ok || errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func workflowMayWriteLockedProductionGeneration(
	ctx context.Context,
	tx pgx.Tx,
	workflowRunID, projectID, generationID, bindingID string,
	bindingRevision int64,
) (bool, error) {
	var allowed bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1
		  FROM workflow_runs run
		  JOIN project_video_production_rebuilds rebuild
		    ON rebuild.workflow_run_id = run.root_workflow_run_id
		   AND rebuild.project_id = run.project_id
		  WHERE run.id = $1
		    AND run.project_id = $2
		    AND run.workflow_type = 'video_production_rebuild_episode'
		    AND run.production_generation_id = $3
		    AND run.video_production_binding_id = $4
		    AND run.video_production_binding_revision = $5
		    AND rebuild.target_generation_id = run.production_generation_id
		    AND rebuild.target_binding_id = run.video_production_binding_id
		    AND rebuild.status IN ('running', 'storyboard_required')
		)
	`, workflowRunID, projectID, generationID, bindingID, bindingRevision).Scan(&allowed)
	return allowed, err
}

func isWorkflowWriteFenced(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrWorkflowWriteFenced) ||
		errors.Is(err, workflowWriteFenceCause) ||
		strings.Contains(err.Error(), workflowWriteFenceMessage)
}

func discardWorkflowResult(ctx context.Context, db txBeginner, execution NodeExecution, reason string) error {
	if db == nil || strings.TrimSpace(execution.NodeRunID) == "" {
		return ErrWorkflowWriteFenced
	}
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	tx, err := db.Begin(persistCtx)
	if err != nil {
		return ErrWorkflowWriteFenced
	}
	defer tx.Rollback(persistCtx)
	if err := insertWorkflowResultDiscardedTx(persistCtx, tx, execution, reason); err != nil {
		return ErrWorkflowWriteFenced
	}
	if err := tx.Commit(persistCtx); err != nil {
		return ErrWorkflowWriteFenced
	}
	return ErrWorkflowWriteFenced
}

func finalizeWorkflowActivityError(ctx context.Context, db txBeginner, execution NodeExecution, err error) error {
	if err == nil || !execution.valid() {
		return err
	}
	if isWorkflowWriteFenced(err) {
		return discardWorkflowResult(ctx, db, execution, err.Error())
	}
	code, message := workflowErrorFields(err, codeActivityFailed)
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	_ = FailNodeRun(persistCtx, db, execution, code, message)
	return err
}

func insertWorkflowResultDiscardedTx(ctx context.Context, tx pgx.Tx, execution NodeExecution, reason string) error {
	runCtx, err := lockNodeRunContextByID(ctx, tx, execution.NodeRunID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = workflowWriteFenceMessage
	}
	currentGenerationID := ""
	currentBindingID := ""
	var currentBindingRevision int64
	_ = tx.QueryRow(ctx, `
		SELECT project.active_video_production_generation_id::text,
		       generation.binding_id::text,
		       binding.revision
		FROM projects project
		JOIN project_video_production_generations generation
		  ON generation.id = project.active_video_production_generation_id
		 AND generation.project_id = project.id
		JOIN project_video_production_bindings binding
		  ON binding.id = generation.binding_id
		 AND binding.project_id = project.id
		WHERE project.id = $1
	`, runCtx.ProjectID).Scan(&currentGenerationID, &currentBindingID, &currentBindingRevision)
	reasonCode := CodeWorkflowResultDiscarded
	if execution.ProductionGenerationID != "" && currentGenerationID != "" && execution.ProductionGenerationID != currentGenerationID {
		reasonCode = videoproduction.CodeGenerationMismatch
	}
	return insertEvent(ctx, tx, runCtx.OrganizationID, runCtx.ProjectID, "workflow.result.discarded", "workflow_node_run", execution.NodeRunID, mustJSON(map[string]any{
		"workflowRunId":                   runCtx.WorkflowRunID,
		"nodeRunId":                       execution.NodeRunID,
		"nodeKey":                         runCtx.NodeKey,
		"attemptGeneration":               execution.AttemptGeneration,
		"executionToken":                  execution.ExecutionToken,
		"currentAttemptGeneration":        runCtx.AttemptGeneration,
		"currentExecutionToken":           runCtx.ExecutionToken,
		"workflowStatus":                  runCtx.WorkflowStatus,
		"nodeStatus":                      runCtx.NodeStatus,
		"errorCode":                       CodeWorkflowResultDiscarded,
		"reasonCode":                      reasonCode,
		"productionGenerationId":          execution.ProductionGenerationID,
		"videoProductionBindingId":        execution.VideoProductionBindingID,
		"bindingRevision":                 execution.VideoProductionBindingRevision,
		"currentProductionGenerationId":   currentGenerationID,
		"currentVideoProductionBindingId": currentBindingID,
		"currentBindingRevision":          currentBindingRevision,
		"reason":                          reason,
	}))
}

func lockWorkflowRunContext(ctx context.Context, tx pgx.Tx, workflowRunID string) (nodeRunContext, error) {
	var projectID string
	if err := tx.QueryRow(ctx, `SELECT project_id::text FROM workflow_runs WHERE id = $1`, workflowRunID).Scan(&projectID); err != nil {
		return nodeRunContext{}, err
	}
	if err := lockProjectRow(ctx, tx, projectID); err != nil {
		return nodeRunContext{}, err
	}

	var runCtx nodeRunContext
	err := tx.QueryRow(ctx, `
		SELECT organization_id, project_id, id::text, '', status,
		       production_generation_id::text, video_production_binding_id::text,
		       video_production_binding_revision
		FROM workflow_runs
		WHERE id = $1
		FOR UPDATE
	`, workflowRunID).Scan(
		&runCtx.OrganizationID, &runCtx.ProjectID, &runCtx.WorkflowRunID, &runCtx.NodeKey, &runCtx.WorkflowStatus,
		&runCtx.ProductionGenerationID, &runCtx.VideoProductionBindingID, &runCtx.VideoProductionBindingRevision,
	)
	return runCtx, err
}

func lockProjectRow(ctx context.Context, tx pgx.Tx, projectID string) error {
	var lockedProjectID string
	return tx.QueryRow(ctx, `SELECT id::text FROM projects WHERE id = $1 FOR UPDATE`, projectID).Scan(&lockedProjectID)
}

func nullableCancelReason(reason string) any {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil
	}
	return reason
}

type txBeginner interface {
	Begin(context.Context) (pgx.Tx, error)
}
