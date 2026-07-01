package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Einzieg/cineweave/internal/workflows"
	"go.temporal.io/sdk/client"
)

func TestRegenerateSceneStoryboardReturnsWorkflowRunID(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	scriptID := seed.insertActiveScript(t)
	versionID := seed.currentScriptVersionID(t, scriptID)
	sceneID := seed.insertScriptScene(t, scriptID, versionID, 1, "approved", "fresh")
	temporal := &fakeTemporalClient{}
	server := New(seed.pool, seed.authService, nil, nil, nil)
	server.temporal = temporal

	var response RegenerateResponse
	doAPISuccess(t, server.Handler(), http.MethodPost, "/api/projects/"+seed.projectID+"/regenerate", seed.ownerToken, seed.organizationID, map[string]any{
		"targetType": "scene_storyboard",
		"targetId":   sceneID,
		"options": map[string]any{
			"force": true,
		},
	}, &response)

	if response.WorkflowRunID == "" || response.WorkflowType != "regenerate_scene_storyboard" || response.TargetID != sceneID || response.Status != "queued" {
		t.Fatalf("regenerate response = %+v", response)
	}
	if temporal.executeCount != 1 || temporal.options.TaskQueue != workflows.ScriptTaskQueue {
		t.Fatalf("temporal call count=%d options=%+v", temporal.executeCount, temporal.options)
	}
	if len(temporal.args) != 1 {
		t.Fatalf("temporal args len = %d, want 1", len(temporal.args))
	}
	workflowInput, ok := temporal.args[0].(workflows.TextToStoryboardInput)
	if !ok {
		t.Fatalf("workflow input type = %T", temporal.args[0])
	}
	if workflowInput.WorkflowRunID != response.WorkflowRunID || workflowInput.ProjectID != seed.projectID {
		t.Fatalf("workflow input = %+v response=%+v", workflowInput, response)
	}
	var options struct {
		TargetID string `json:"targetId"`
		Force    bool   `json:"force"`
	}
	if err := json.Unmarshal(workflowInput.Input, &options); err != nil {
		t.Fatalf("decode workflow options: %v", err)
	}
	if options.TargetID != sceneID || !options.Force {
		t.Fatalf("workflow options = %+v", options)
	}
}

type fakeTemporalClient struct {
	executeCount int
	options      client.StartWorkflowOptions
	workflow     any
	args         []any
}

func (c *fakeTemporalClient) ExecuteWorkflow(ctx context.Context, options client.StartWorkflowOptions, workflow interface{}, args ...interface{}) (client.WorkflowRun, error) {
	c.executeCount++
	c.options = options
	c.workflow = workflow
	c.args = append([]any(nil), args...)
	return fakeWorkflowRun{id: options.ID, runID: "fake-run"}, nil
}

func (c *fakeTemporalClient) CancelWorkflow(ctx context.Context, workflowID string, runID string) error {
	return nil
}

func (c *fakeTemporalClient) SignalWorkflow(ctx context.Context, workflowID string, runID string, signalName string, arg interface{}) error {
	return nil
}

type fakeWorkflowRun struct {
	id    string
	runID string
}

func (r fakeWorkflowRun) GetID() string {
	return r.id
}

func (r fakeWorkflowRun) GetRunID() string {
	return r.runID
}

func (r fakeWorkflowRun) Get(ctx context.Context, valuePtr interface{}) error {
	return nil
}

func (r fakeWorkflowRun) GetWithOptions(ctx context.Context, valuePtr interface{}, options client.WorkflowRunGetOptions) error {
	return nil
}
