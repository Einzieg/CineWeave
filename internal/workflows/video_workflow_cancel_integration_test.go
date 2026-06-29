package workflows

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Einzieg/cineweave/internal/db"
	"github.com/Einzieg/cineweave/internal/provider"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestVideoProductionWorkflowCancel(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run video workflow cancel integration test")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for video workflow cancel integration test")
	}

	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer pool.Close()

	orgID, userID, projectID, workflowRunID, textModelID, imageModelID := seedWorkflowGatewayIntegrationData(t, ctx, pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
	})
	videoModelID := seedWorkflowVideoProfile(t, ctx, pool, orgID, userID, textModelID)
	var cancelCalls int32
	gateway := httptest.NewServer(mockVideoWorkflowCancelGateway(t, textModelID, imageModelID, videoModelID, &cancelCalls))
	defer gateway.Close()

	activities := NewActivities(pool, newWorkflowMemoryStorage(), &provider.GatewayClient{
		BaseURL: gateway.URL,
		Token:   "workflow-service-token",
		Client:  gateway.Client(),
	})
	input := TextToStoryboardInput{
		OrganizationID: orgID,
		ProjectID:      projectID,
		WorkflowRunID:  workflowRunID,
		Prompt:         "A quiet train station at sunrise with cinematic lighting.",
		CreatedBy:      userID,
	}

	storyboard, err := activities.GenerateStoryboardText(ctx, generateStoryboardTextInput(input))
	if err != nil {
		t.Fatalf("GenerateStoryboardText: %v", err)
	}
	imageOutput, err := activities.GenerateStoryboardImage(ctx, GenerateStoryboardImageInput{
		OrganizationID:         input.OrganizationID,
		ProjectID:              input.ProjectID,
		WorkflowRunID:          input.WorkflowRunID,
		Prompt:                 input.Prompt,
		CreatedBy:              input.CreatedBy,
		StoryboardArtifactID:   storyboard.StoryboardArtifactID,
		Storyboard:             storyboard.Storyboard,
		StoryboardProviderCall: storyboard.ProviderCallID,
	})
	if err != nil {
		t.Fatalf("GenerateStoryboardImage: %v", err)
	}
	createOutput, err := activities.CreateStoryboardVideoTask(ctx, CreateStoryboardVideoTaskInput{
		OrganizationID:       input.OrganizationID,
		ProjectID:            input.ProjectID,
		WorkflowRunID:        input.WorkflowRunID,
		CreatedBy:            input.CreatedBy,
		StoryboardArtifactID: storyboard.StoryboardArtifactID,
		ImageArtifactID:      imageOutput.ImageArtifactID,
		ImageMediaFileID:     imageOutput.ImageMediaFileID,
		ImageStorageKey:      imageOutput.ImageStorageKey,
		Prompt:               input.Prompt,
		VideoPrompt:          selectVideoPrompt(storyboard.Storyboard, input.Prompt, 5),
		Duration:             5,
		AspectRatio:          "16:9",
		Resolution:           "720p",
		Storyboard:           storyboard.Storyboard,
	})
	if err != nil {
		t.Fatalf("CreateStoryboardVideoTask: %v", err)
	}
	if err := MarkWorkflowCancelling(ctx, pool, workflowRunID, "user clicked cancel"); err != nil {
		t.Fatalf("MarkWorkflowCancelling: %v", err)
	}
	cancelOutput, err := activities.CancelStoryboardVideoTask(ctx, CancelStoryboardVideoTaskInput{
		OrganizationID:      input.OrganizationID,
		ProjectID:           input.ProjectID,
		WorkflowRunID:       input.WorkflowRunID,
		NodeRunID:           createOutput.NodeRunID,
		ProviderAsyncTaskID: createOutput.ProviderAsyncTaskID,
		ExternalTaskID:      createOutput.ExternalTaskID,
		Reason:              "user clicked cancel",
	})
	if err != nil {
		t.Fatalf("CancelStoryboardVideoTask: %v", err)
	}
	if err := activities.CancelVideoProductionWorkflow(ctx, input, cancelOutput, "user clicked cancel"); err != nil {
		t.Fatalf("CancelVideoProductionWorkflow: %v", err)
	}
	if atomic.LoadInt32(&cancelCalls) != 1 {
		t.Fatalf("gateway cancel calls = %d, want 1", cancelCalls)
	}
	assertWorkflowRunStatus(t, ctx, pool, workflowRunID, "cancelled")
	assertWorkflowNodeStatus(t, ctx, pool, workflowRunID, nodeGenerateStoryboardVideoKey, "cancelled")
	for _, eventType := range []string{"workflow.run.cancelling", "workflow.node.cancelled", "workflow.run.cancelled", "provider.video.task.cancelled"} {
		assertWorkflowCancelEventType(t, ctx, pool, orgID, workflowRunID, eventType)
	}
}

func mockVideoWorkflowCancelGateway(t *testing.T, textModelID, imageModelID, videoModelID string, cancelCalls *int32) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer workflow-service-token" {
			t.Fatalf("Authorization header = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/internal/provider/text/generate":
			writeWorkflowGatewayEnvelope(t, w, provider.GatewayTextResponse{
				ProviderCallID: uuid.NewString(),
				ModelID:        textModelID,
				Status:         "succeeded",
				Output:         provider.GatewayTextOutput{Text: `{"title":"Cancel","shots":[{"imagePrompt":"station","videoPrompt":"station video","camera":"push","motion":"mist","mood":"calm"}]}`},
			})
		case "/internal/provider/image/generate":
			var req provider.GatewayImageRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode image request: %v", err)
			}
			writeWorkflowGatewayEnvelope(t, w, provider.GatewayImageResponse{
				ProviderCallID: uuid.NewString(),
				ModelID:        imageModelID,
				Status:         "succeeded",
				Output: provider.GatewayImageOutput{
					ArtifactID:  uuid.NewString(),
					MediaFileID: uuid.NewString(),
					StorageKey:  "org/test/project/test/provider-images/2026/06/image.png",
					MimeType:    "image/png",
				},
			})
		case "/internal/provider/video/create-task":
			writeWorkflowGatewayEnvelope(t, w, provider.GatewayVideoCreateTaskResponse{
				ProviderCallID:      uuid.NewString(),
				ProviderAsyncTaskID: uuid.NewString(),
				ExternalTaskID:      "external-video-task",
				ModelID:             videoModelID,
				Status:              "running",
			})
		case "/internal/provider/video/cancel-task":
			atomic.AddInt32(cancelCalls, 1)
			var req provider.GatewayVideoCancelTaskRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode cancel request: %v", err)
			}
			writeWorkflowGatewayEnvelope(t, w, provider.GatewayVideoCancelTaskResponse{
				ProviderCallID:      uuid.NewString(),
				ProviderAsyncTaskID: req.ProviderAsyncTaskID,
				ExternalTaskID:      req.ExternalTaskID,
				Status:              "cancelled",
			})
		default:
			http.NotFound(w, r)
		}
	})
}

func assertWorkflowRunStatus(t *testing.T, ctx context.Context, pool txQueryer, workflowRunID, wantStatus string) {
	t.Helper()
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM workflow_runs WHERE id = $1`, workflowRunID).Scan(&status); err != nil {
		t.Fatalf("select workflow run status: %v", err)
	}
	if status != wantStatus {
		t.Fatalf("workflow status = %q, want %q", status, wantStatus)
	}
}

func assertWorkflowNodeStatus(t *testing.T, ctx context.Context, pool txQueryer, workflowRunID, nodeKey, wantStatus string) {
	t.Helper()
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM workflow_node_runs WHERE workflow_run_id = $1 AND node_key = $2`, workflowRunID, nodeKey).Scan(&status); err != nil {
		t.Fatalf("select workflow node status: %v", err)
	}
	if status != wantStatus {
		t.Fatalf("node status = %q, want %q", status, wantStatus)
	}
}

func assertWorkflowCancelEventType(t *testing.T, ctx context.Context, pool txQueryer, orgID, workflowRunID, eventType string) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM event_outbox
		WHERE organization_id = $1
		  AND event_type = $2
		  AND (payload->>'workflowRunId' = $3 OR aggregate_id = $3::uuid)
	`, orgID, eventType, workflowRunID).Scan(&count); err != nil {
		t.Fatalf("select workflow cancel event: %v", err)
	}
	if count == 0 {
		t.Fatalf("missing event %s", eventType)
	}
}

type txQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}
