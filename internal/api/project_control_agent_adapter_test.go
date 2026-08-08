package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Einzieg/cineweave/internal/projectcontrol"
)

func TestProjectControlAgentDescriptorsExposeProjectAndIdempotencyContract(t *testing.T) {
	descriptors, tools, err := projectControlAgentDescriptors()
	if err != nil {
		t.Fatalf("build project control agent descriptors: %v", err)
	}
	if len(descriptors) == 0 || len(tools) != len(descriptors) {
		t.Fatalf("descriptors=%d tools=%d", len(descriptors), len(tools))
	}
	writeCount := 0
	for _, descriptor := range descriptors {
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Required   []string                   `json:"required"`
		}
		if err := json.Unmarshal(descriptor.InputSchema, &schema); err != nil {
			t.Fatalf("decode %s schema: %v", descriptor.Name, err)
		}
		if descriptor.ExportToMCP {
			if _, exists := schema.Properties["projectId"]; !exists || !containsControlField(schema.Required, "projectId") {
				t.Fatalf("MCP descriptor %s does not require projectId", descriptor.Name)
			}
		}
		if descriptor.ReadOnly {
			continue
		}
		writeCount++
		if descriptor.ExecutionMode == projectcontrol.ExecutionModeSync {
			if !projectControlHasSharedSyncAction(descriptor.Name) {
				t.Fatalf("write descriptor %s is sync without a shared transaction action", descriptor.Name)
			}
		} else if descriptor.ExecutionMode != projectcontrol.ExecutionModeAsyncCommand && descriptor.ExecutionMode != projectcontrol.ExecutionModeWorkflow {
			t.Fatalf("write descriptor %s mode=%s", descriptor.Name, descriptor.ExecutionMode)
		}
		if descriptor.ExportToMCP {
			if _, exists := schema.Properties["idempotencyKey"]; !exists || !containsControlField(schema.Required, "idempotencyKey") {
				t.Fatalf("MCP write descriptor %s does not require idempotencyKey", descriptor.Name)
			}
		}
	}
	if writeCount == 0 {
		t.Fatal("expected at least one writable Agent descriptor")
	}
}

func TestProjectControlShotStatusDescriptorIsReadOnly(t *testing.T) {
	descriptors, _, err := projectControlAgentDescriptors()
	if err != nil {
		t.Fatalf("projectControlAgentDescriptors: %v", err)
	}
	for _, descriptor := range descriptors {
		if descriptor.Name != "shot.status" {
			continue
		}
		if !descriptor.ReadOnly {
			t.Fatal("shot.status must remain a read-only project control action")
		}
		if descriptor.ExecutionMode != projectcontrol.ExecutionModeSync {
			t.Fatalf("shot.status execution mode = %q, want %q", descriptor.ExecutionMode, projectcontrol.ExecutionModeSync)
		}
		return
	}
	t.Fatal("shot.status descriptor is missing")
}

func TestProjectControlScriptReadsUseSharedDomainAndBoundedContent(t *testing.T) {
	contracts, err := ProjectControlActionContracts()
	if err != nil {
		t.Fatalf("build project control contracts: %v", err)
	}
	found := map[string]projectcontrol.ActionContract{}
	for _, contract := range contracts {
		if contract.Descriptor.Name == "script.list" || contract.Descriptor.Name == "script.get" || contract.Descriptor.Name == "content.read" {
			found[contract.Descriptor.Name] = contract
		}
	}
	for _, name := range []string{"script.list", "script.get"} {
		contract, exists := found[name]
		if !exists {
			t.Fatalf("%s contract is missing", name)
		}
		if contract.ImplementationKind != projectcontrol.ImplementationSharedDomain || contract.MigrationStatus != projectcontrol.MigrationStatusMigrated {
			t.Fatalf("%s implementation=%s migration=%s", name, contract.ImplementationKind, contract.MigrationStatus)
		}
	}
	var getSchema struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(found["script.get"].Descriptor.InputSchema, &getSchema); err != nil {
		t.Fatalf("decode script.get schema: %v", err)
	}
	if !containsControlField(getSchema.Required, "projectId") || !containsControlField(getSchema.Required, "scriptId") {
		t.Fatalf("script.get required = %#v", getSchema.Required)
	}
	if !strings.Contains(string(found["content.read"].Descriptor.InputSchema), `"script_version"`) {
		t.Fatalf("content.read does not expose script_version: %s", found["content.read"].Descriptor.InputSchema)
	}

	result := scriptGetAgentResult(map[string]any{"scriptId": "script-1"}, scriptGetActionPage{
		Script: scriptActionSummary{ID: "script-1", Title: "第一集剧本", Revision: 2},
		Version: &scriptVersionActionSummary{
			ID: "version-1", Version: 1, ContentHash: strings.Repeat("a", 64),
			ContentTarget: projectControlContentTarget{TargetType: "script_version", TargetID: "version-1"},
		},
		Episodes: []scriptEpisodeActionSummary{{
			ID: "episode-1", EpisodeIndex: 1, EpisodeTitle: "第一集", Revision: 3,
			ContentHash:   strings.Repeat("b", 64),
			ContentTarget: projectControlContentTarget{TargetType: "script_episode", TargetID: "episode-1"},
		}},
		EpisodeLimit: 20,
	})
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal script.get result: %v", err)
	}
	if strings.Contains(string(raw), "正文内容") || !strings.Contains(string(raw), `"targetType":"script_episode"`) {
		t.Fatalf("script.get result is not bounded/content-addressable: %s", raw)
	}
}

func TestProjectControlSourceReadsUseSharedDomainAndRequireExactSource(t *testing.T) {
	contracts, err := ProjectControlActionContracts()
	if err != nil {
		t.Fatalf("build project control contracts: %v", err)
	}
	found := map[string]projectcontrol.ActionContract{}
	for _, contract := range contracts {
		if contract.Descriptor.Name == "source.create" || contract.Descriptor.Name == "source.list" || contract.Descriptor.Name == "source.list_chapters" {
			found[contract.Descriptor.Name] = contract
		}
	}
	for _, name := range []string{"source.create", "source.list", "source.list_chapters"} {
		contract, exists := found[name]
		if !exists {
			t.Fatalf("%s contract is missing", name)
		}
		if contract.ImplementationKind != projectcontrol.ImplementationSharedDomain || contract.MigrationStatus != projectcontrol.MigrationStatusMigrated {
			t.Fatalf("%s implementation=%s migration=%s", name, contract.ImplementationKind, contract.MigrationStatus)
		}
	}
	var schema struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(found["source.list_chapters"].Descriptor.InputSchema, &schema); err != nil {
		t.Fatalf("decode source.list_chapters schema: %v", err)
	}
	if !containsControlField(schema.Required, "projectId") || !containsControlField(schema.Required, "sourceId") {
		t.Fatalf("source.list_chapters required = %#v", schema.Required)
	}
}

func TestProjectControlExternalActionUsesSharedRuntimeWithoutCreatingAgentTask(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()
	t.Setenv("CINEWEAVE_AGENT_MAX_ACTIVE_TASKS_PER_PROJECT", "1")

	descriptor, exists := seed.apiServer.projectControl.registry.Get("workflow.start")
	if !exists {
		t.Fatal("workflow.start descriptor was not registered")
	}
	registration, exists := seed.apiServer.projectControl.runtime.Get(descriptor.Name, descriptor.Version)
	if !exists {
		t.Fatal("workflow.start shared runtime was not registered")
	}
	if _, ok := registration.Handler.(*projectControlSharedRuntimeHandler); !ok {
		t.Fatalf("workflow.start runtime handler = %T, want shared runtime", registration.Handler)
	}
	command, _, err := seed.apiServer.projectControl.repository.Create(seed.ctx, projectcontrol.CreateCommand{
		OrganizationID: seed.organizationID,
		ProjectID:      seed.projectID,
		ActorUserID:    seed.ownerUserID,
		ControllerType: projectcontrol.ControllerManual,
		Descriptor:     descriptor,
		Input:          json.RawMessage(`{}`),
		IdempotencyKey: "test-shared-runtime-no-agent-task",
	})
	if err != nil {
		t.Fatalf("create project control command: %v", err)
	}

	var list struct {
		Items []AgentTask `json:"items"`
	}
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+seed.projectID+"/agent/tasks", seed.ownerToken, seed.organizationID, nil, &list)
	for _, task := range list.Items {
		if task.ID == command.ID {
			t.Fatalf("external project-control command created an assistant task: %+v", task)
		}
	}
	var taskCount int
	if err := seed.pool.QueryRow(seed.ctx, `SELECT count(*) FROM agent_tasks WHERE id = $1`, command.ID).Scan(&taskCount); err != nil {
		t.Fatalf("count external command agent tasks: %v", err)
	}
	if taskCount != 0 {
		t.Fatalf("external command created %d agent task rows", taskCount)
	}
	if err := seed.apiServer.enforceAgentProjectTaskConcurrency(seed.ctx, seed.projectID); err != nil {
		t.Fatalf("external project-control command consumed assistant concurrency: %v", err)
	}
	seed.insertAgentTask(t, "running")
	if err := seed.apiServer.enforceAgentProjectTaskConcurrency(seed.ctx, seed.projectID); err == nil {
		t.Fatal("visible active assistant task did not consume concurrency")
	}
}

func TestCancellingAgentTaskCancelsLinkedProjectControlCommandFirst(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	taskID := seed.insertAgentTask(t, "running")
	stepID := seed.insertAgentStep(t, taskID, 1, "source.delete", "write", "running")
	descriptor, exists := seed.apiServer.projectControl.registry.Get("source.delete")
	if !exists {
		t.Fatal("source.delete descriptor was not registered")
	}
	command, _, err := seed.apiServer.projectControl.repository.Create(seed.ctx, projectcontrol.CreateCommand{
		OrganizationID: seed.organizationID,
		WorkspaceID:    seed.workspaceID,
		ProjectID:      seed.projectID,
		ActorUserID:    seed.ownerUserID,
		ControllerType: projectcontrol.ControllerEmbeddedAgent,
		AgentTaskID:    taskID,
		AgentStepID:    stepID,
		Descriptor:     descriptor,
		Input:          json.RawMessage(`{}`),
		IdempotencyKey: "test-cancel-linked-command",
	})
	if err != nil {
		t.Fatalf("create linked project control command: %v", err)
	}

	project := Project{ID: seed.projectID, OrganizationID: seed.organizationID, WorkspaceID: seed.workspaceID}
	cancelledTask, err := seed.apiServer.cancelAgentTaskCore(seed.ctx, project, taskID, seed.ownerUserID, "test cancellation")
	if err != nil {
		t.Fatalf("cancel agent task: %v", err)
	}
	if cancelledTask.Status != "cancelled" {
		t.Fatalf("task status=%s, want cancelled", cancelledTask.Status)
	}
	got, err := seed.apiServer.projectControl.repository.Get(seed.ctx, command.ID)
	if err != nil {
		t.Fatalf("read linked command: %v", err)
	}
	if got.Status != projectcontrol.CommandCancelled || got.CancellationRequestedAt == nil {
		t.Fatalf("linked command status=%s cancellationRequestedAt=%v", got.Status, got.CancellationRequestedAt)
	}
	var linkedCommandID, stepStatus string
	if err := seed.pool.QueryRow(seed.ctx, `
		SELECT project_control_command_id::text, status
		FROM agent_steps
		WHERE id = $1
	`, stepID).Scan(&linkedCommandID, &stepStatus); err != nil {
		t.Fatalf("read linked agent step: %v", err)
	}
	if linkedCommandID != command.ID || stepStatus != "cancelled" {
		t.Fatalf("linkedCommandID=%s stepStatus=%s", linkedCommandID, stepStatus)
	}
}

func containsControlField(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
