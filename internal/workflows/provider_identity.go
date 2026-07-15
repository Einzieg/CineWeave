package workflows

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type workflowExecutionIdentity struct {
	WorkflowRunID     string
	RootWorkflowRunID string
	ExecutionToken    string
	AttemptGeneration int
}

func loadWorkflowExecutionIdentity(ctx context.Context, pool *pgxpool.Pool, workflowRunID string) (workflowExecutionIdentity, error) {
	var identity workflowExecutionIdentity
	err := pool.QueryRow(ctx, `
		SELECT id::text, COALESCE(root_workflow_run_id, id)::text,
		       execution_token::text, attempt_generation
		FROM workflow_runs
		WHERE id = $1
	`, workflowRunID).Scan(
		&identity.WorkflowRunID,
		&identity.RootWorkflowRunID,
		&identity.ExecutionToken,
		&identity.AttemptGeneration,
	)
	return identity, err
}

func stableProviderRequestKey(namespace string, identity workflowExecutionIdentity, nodeKey, inputVersion string) string {
	namespace = strings.TrimSpace(namespace)
	nodeKey = strings.TrimSpace(nodeKey)
	inputVersion = strings.TrimSpace(inputVersion)
	if identity.AttemptGeneration <= 0 {
		identity.AttemptGeneration = 1
	}
	rootID := strings.TrimSpace(identity.RootWorkflowRunID)
	if rootID == "" {
		rootID = strings.TrimSpace(identity.WorkflowRunID)
	}
	digest := sha256.Sum256([]byte(strings.Join([]string{
		"cineweave-provider-key-v1", namespace, rootID, nodeKey,
		fmt.Sprintf("%d", identity.AttemptGeneration), inputVersion,
	}, "\x00")))
	return fmt.Sprintf("%s:%s:%s:g%d:%s", namespace, rootID, nodeKey, identity.AttemptGeneration, hex.EncodeToString(digest[:12]))
}
