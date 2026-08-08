package api

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
)

func TestProjectControlWorkflowStartReusesRunAfterDispatchCrash(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()
	configureTimelineTestProject(t, seed.apiServer.Handler(), seed, "Codex Workflow Project")
	seed.apiServer.temporal = &fakeTemporalClient{}

	identity := projectControlTestCodexIdentity(t, seed)
	created := executeProjectControlTestAction(t, seed, identity, "workflow.start", map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "workflow-start-crash-recovery",
		"workflowType": "text_to_storyboard", "input": map[string]any{"prompt": "生成第一集分镜"},
	})
	command, err := seed.apiServer.projectControl.repository.Get(seed.ctx, created.CommandID)
	if err != nil {
		t.Fatalf("read workflow command: %v", err)
	}
	project, err := projectByIDForControl(seed.ctx, seed.apiServer, seed.projectID)
	if err != nil {
		t.Fatalf("read project: %v", err)
	}
	principal := auth.Principal{UserID: seed.ownerUserID, OrganizationID: seed.organizationID}

	// Simulate a worker crash after the domain transaction committed but before
	// the dispatcher persisted the workflow link on the command.
	first, err := seed.apiServer.executeWorkflowStartAsyncAction(seed.ctx, principal, project, command, command.Input)
	if err != nil {
		t.Fatalf("start workflow before simulated crash: %v", err)
	}
	firstRunID := workflowRunIDFromAgentResult(t, first)

	dispatchProjectControlCommand(t, seed)
	command, err = seed.apiServer.projectControl.repository.Get(seed.ctx, created.CommandID)
	if err != nil {
		t.Fatalf("reload workflow command: %v", err)
	}
	if command.Status != projectcontrol.CommandWaitingWorkflow {
		t.Fatalf("command status=%s, want %s", command.Status, projectcontrol.CommandWaitingWorkflow)
	}

	var runCount, linkCount int
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT COUNT(*)
		FROM workflow_runs
		WHERE project_id = $1
		  AND workflow_type = 'text_to_storyboard'
		  AND input->'input'->>'projectControlCommandId' = $2
	`, seed.projectID, created.CommandID).Scan(&runCount); err != nil {
		t.Fatalf("count workflow runs: %v", err)
	}
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT COUNT(*)
		FROM project_control_command_workflows
		WHERE command_id = $1 AND workflow_run_id = $2
	`, created.CommandID, firstRunID).Scan(&linkCount); err != nil {
		t.Fatalf("count workflow links: %v", err)
	}
	if runCount != 1 || linkCount != 1 {
		t.Fatalf("workflow runs=%d links=%d, want 1/1", runCount, linkCount)
	}
}

func TestProjectControlTimelineComposeReusesRunAfterDispatchCrash(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()
	configureTimelineTestProject(t, seed.apiServer.Handler(), seed, "Codex Compose Project")
	seed.apiServer.temporal = &fakeTemporalClient{}

	identity := projectControlTestCodexIdentity(t, seed)
	createdTimeline := executeProjectControlTestAction(t, seed, identity, "timeline.create", map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "timeline-for-compose",
		"title": "待合成时间线", "aspectRatio": "16:9", "resolution": "720p",
	})
	var timelineData struct {
		Timeline ProjectTimeline `json:"timeline"`
	}
	decodeProjectControlResultData(t, createdTimeline, &timelineData)

	created := executeProjectControlTestAction(t, seed, identity, "timeline.compose", map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "timeline-compose-crash-recovery",
		"timelineId": timelineData.Timeline.ID,
	})
	command, err := seed.apiServer.projectControl.repository.Get(seed.ctx, created.CommandID)
	if err != nil {
		t.Fatalf("read compose command: %v", err)
	}
	project, err := projectByIDForControl(seed.ctx, seed.apiServer, seed.projectID)
	if err != nil {
		t.Fatalf("read project: %v", err)
	}
	principal := auth.Principal{UserID: seed.ownerUserID, OrganizationID: seed.organizationID}

	first, err := seed.apiServer.executeTimelineComposeAsyncAction(seed.ctx, principal, project, command, command.Input)
	if err != nil {
		t.Fatalf("compose timeline before simulated crash: %v", err)
	}
	firstRunID := workflowRunIDFromAgentResult(t, first)
	dispatchProjectControlCommand(t, seed)

	var runCount, linkCount int
	var timelineWorkflowRunID string
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT COUNT(*)
		FROM workflow_runs
		WHERE project_id = $1
		  AND workflow_type = 'compose_timeline'
		  AND input->'input'->>'projectControlCommandId' = $2
	`, seed.projectID, created.CommandID).Scan(&runCount); err != nil {
		t.Fatalf("count compose runs: %v", err)
	}
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT COUNT(*)
		FROM project_control_command_workflows
		WHERE command_id = $1 AND workflow_run_id = $2
	`, created.CommandID, firstRunID).Scan(&linkCount); err != nil {
		t.Fatalf("count compose links: %v", err)
	}
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT workflow_run_id::text FROM project_timelines WHERE id = $1
	`, timelineData.Timeline.ID).Scan(&timelineWorkflowRunID); err != nil {
		t.Fatalf("read timeline workflow: %v", err)
	}
	if runCount != 1 || linkCount != 1 || timelineWorkflowRunID != firstRunID {
		t.Fatalf("compose runs=%d links=%d timelineRun=%s want 1/1/%s", runCount, linkCount, timelineWorkflowRunID, firstRunID)
	}
}

func TestProjectControlWorkflowCancelStopsPendingStartAndReplays(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()
	configureTimelineTestProject(t, seed.apiServer.Handler(), seed, "Codex Cancel Project")
	seed.apiServer.temporal = &fakeTemporalClient{}

	identity := projectControlTestCodexIdentity(t, seed)
	started := executeProjectControlTestAction(t, seed, identity, "workflow.start", map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "workflow-to-cancel",
		"workflowType": "text_to_storyboard", "input": map[string]any{"prompt": "生成待取消分镜"},
	})
	dispatchProjectControlCommand(t, seed)
	var workflowRunID string
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT workflow_run_id::text
		FROM project_control_command_workflows
		WHERE command_id = $1
	`, started.CommandID).Scan(&workflowRunID); err != nil {
		t.Fatalf("read started workflow link: %v", err)
	}

	cancelInput := map[string]any{
		"projectId": seed.projectID, "idempotencyKey": "workflow-cancel-once",
		"workflowRunId": workflowRunID, "reason": "用户取消测试工作流",
	}
	cancelled := executeProjectControlTestAction(t, seed, identity, "workflow.cancel", cancelInput)
	dispatchProjectControlCommand(t, seed)
	replayed := executeProjectControlTestAction(t, seed, identity, "workflow.cancel", cancelInput)
	if replayed.CommandID != cancelled.CommandID {
		t.Fatalf("replayed cancel command=%s want=%s", replayed.CommandID, cancelled.CommandID)
	}

	var runStatus, outboxStatus, commandStatus string
	if err := seed.pool.QueryRow(seed.ctx, `SELECT status FROM workflow_runs WHERE id = $1`, workflowRunID).Scan(&runStatus); err != nil {
		t.Fatalf("read cancelled workflow: %v", err)
	}
	if err := seed.pool.QueryRow(seed.ctx, `SELECT status FROM workflow_start_outbox WHERE workflow_run_id = $1`, workflowRunID).Scan(&outboxStatus); err != nil {
		t.Fatalf("read cancelled workflow outbox: %v", err)
	}
	if err := seed.pool.QueryRow(seed.ctx, `SELECT status FROM project_control_commands WHERE id = $1`, cancelled.CommandID).Scan(&commandStatus); err != nil {
		t.Fatalf("read cancel command: %v", err)
	}
	if runStatus != "cancelled" || outboxStatus != "cancelled" || commandStatus != string(projectcontrol.CommandSucceeded) {
		t.Fatalf("run=%s outbox=%s command=%s, want cancelled/cancelled/succeeded", runStatus, outboxStatus, commandStatus)
	}
}

func dispatchProjectControlCommand(t *testing.T, seed *artifactPreviewSeed) {
	t.Helper()
	dispatcher := projectcontrol.Dispatcher{
		Repository: seed.apiServer.projectControl.repository,
		Registry:   seed.apiServer.projectControl.runtime,
		Owner:      "project-control-workflow-test", ReleaseID: "project-control-workflow-test",
		LeaseDuration: time.Minute, ReconcileDelay: time.Minute,
	}
	processed, err := dispatcher.RunOnce(seed.ctx)
	if err != nil || !processed {
		t.Fatalf("dispatch project control command: processed=%v err=%v", processed, err)
	}
}

func workflowRunIDFromAgentResult(t *testing.T, result agentToolResult) string {
	t.Helper()
	raw, err := json.Marshal(result.Data)
	if err != nil {
		t.Fatalf("marshal workflow result data: %v", err)
	}
	var data struct {
		WorkflowRunID string `json:"workflowRunId"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("decode workflow result data: %v", err)
	}
	if data.WorkflowRunID == "" {
		t.Fatalf("workflow result has no run ID: %+v", result)
	}
	return data.WorkflowRunID
}
