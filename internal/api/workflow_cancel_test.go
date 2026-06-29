package api

import (
	"context"
	"net/http"
	"testing"
)

func TestWorkflowCancel(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	runningID := seed.insertWorkflowRun(t, "running")
	assertAPIErrorCode(t, server, http.MethodPost, "/api/workflow-runs/"+runningID+"/cancel", seed.otherToken, seed.organizationID, map[string]any{"reason": "not mine"}, http.StatusForbidden, "FORBIDDEN")

	var cancelling WorkflowRun
	doAPISuccess(t, server, http.MethodPost, "/api/workflow-runs/"+runningID+"/cancel", seed.ownerToken, seed.organizationID, map[string]any{"reason": "stop spending"}, &cancelling)
	if cancelling.Status != "cancelling" || cancelling.ErrorCode == nil || *cancelling.ErrorCode != "USER_CANCEL_REQUESTED" {
		t.Fatalf("cancelled response = %+v", cancelling)
	}
	assertWorkflowCancelEvent(t, seed, runningID, "workflow.run.cancelling")

	succeededID := seed.insertWorkflowRun(t, "succeeded")
	var succeeded WorkflowRun
	doAPISuccess(t, server, http.MethodPost, "/api/workflow-runs/"+succeededID+"/cancel", seed.ownerToken, seed.organizationID, map[string]any{"reason": "too late"}, &succeeded)
	if succeeded.Status != "succeeded" {
		t.Fatalf("terminal cancel status = %q, want succeeded", succeeded.Status)
	}
}

func (s *artifactPreviewSeed) insertWorkflowRun(t *testing.T, status string) string {
	t.Helper()
	var workflowRunID string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO workflow_runs(organization_id, project_id, temporal_workflow_id, status, input, output, created_by)
		VALUES ($1, $2, $3, $4, '{}', '{}', $5)
		RETURNING id
	`, s.organizationID, s.projectID, "workflow-cancel-"+status+"-"+randomStorageSegment(), status, s.ownerUserID).Scan(&workflowRunID); err != nil {
		t.Fatalf("insert workflow run: %v", err)
	}
	return workflowRunID
}

func assertWorkflowCancelEvent(t *testing.T, seed *artifactPreviewSeed, workflowRunID, eventType string) {
	t.Helper()
	var count int
	if err := seed.pool.QueryRow(context.Background(), `
		SELECT count(*)
		FROM event_outbox
		WHERE organization_id = $1
		  AND aggregate_id = $2::uuid
		  AND event_type = $3
	`, seed.organizationID, workflowRunID, eventType).Scan(&count); err != nil {
		t.Fatalf("select cancel event: %v", err)
	}
	if count == 0 {
		t.Fatalf("missing event %s for workflow %s", eventType, workflowRunID)
	}
}
