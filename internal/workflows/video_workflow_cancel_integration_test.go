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
	"github.com/jackc/pgx/v5/pgxpool"
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
	gateway := httptest.NewServer(mockVideoWorkflowCancelGateway(t, pool, textModelID, imageModelID, videoModelID, &cancelCalls))
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

	_, err = activities.GenerateStoryboardText(ctx, generateStoryboardTextInput(input))
	if err != nil {
		t.Fatalf("GenerateStoryboardText: %v", err)
	}
	shots, err := activities.ListStoryboardShots(ctx, ListStoryboardShotsInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
	})
	if err != nil {
		t.Fatalf("ListStoryboardShots: %v", err)
	}
	if len(shots) != 3 {
		t.Fatalf("shots len = %d, want 3", len(shots))
	}
	firstImage, err := activities.GenerateShotImage(ctx, GenerateShotImageInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		CreatedBy:      input.CreatedBy,
		ShotID:         shots[0].ID,
		ShotIndex:      shots[0].ShotIndex,
		ShotNo:         shots[0].ShotNo,
		WorkflowPrompt: input.Prompt,
		AspectRatio:    "16:9",
	})
	if err != nil {
		t.Fatalf("GenerateShotImage first: %v", err)
	}
	if _, err := activities.CreateShotVideoTask(ctx, CreateShotVideoTaskInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		CreatedBy:      input.CreatedBy,
		ShotID:         shots[0].ID,
		ShotIndex:      shots[0].ShotIndex,
		ShotNo:         shots[0].ShotNo,
		WorkflowPrompt: input.Prompt,
		Duration:       shots[0].Duration,
		AspectRatio:    "16:9",
		Resolution:     "720p",
	}); err != nil {
		t.Fatalf("CreateShotVideoTask first: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE storyboard_shots
		SET status = 'video_succeeded',
		    video_artifact_id = $2,
		    video_media_file_id = $3,
		    video_storage_key = $4
		WHERE id = $1
	`, shots[0].ID, firstImage.ImageArtifactID, firstImage.ImageMediaFileID, "first-video.mp4"); err != nil {
		t.Fatalf("mark first shot succeeded: %v", err)
	}
	secondImage, err := activities.GenerateShotImage(ctx, GenerateShotImageInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		CreatedBy:      input.CreatedBy,
		ShotID:         shots[1].ID,
		ShotIndex:      shots[1].ShotIndex,
		ShotNo:         shots[1].ShotNo,
		WorkflowPrompt: input.Prompt,
		AspectRatio:    "16:9",
	})
	if err != nil {
		t.Fatalf("GenerateShotImage second: %v", err)
	}
	_ = secondImage
	createOutput, err := activities.CreateShotVideoTask(ctx, CreateShotVideoTaskInput{
		OrganizationID: input.OrganizationID,
		ProjectID:      input.ProjectID,
		WorkflowRunID:  input.WorkflowRunID,
		CreatedBy:      input.CreatedBy,
		ShotID:         shots[1].ID,
		ShotIndex:      shots[1].ShotIndex,
		ShotNo:         shots[1].ShotNo,
		WorkflowPrompt: input.Prompt,
		Duration:       shots[1].Duration,
		AspectRatio:    "16:9",
		Resolution:     "720p",
	})
	if err != nil {
		t.Fatalf("CreateShotVideoTask second: %v", err)
	}
	if err := MarkWorkflowCancelling(ctx, pool, workflowRunID, "user clicked cancel"); err != nil {
		t.Fatalf("MarkWorkflowCancelling: %v", err)
	}
	cancelOutput, err := activities.CancelShotVideoTask(ctx, CancelShotVideoTaskInput{
		OrganizationID:      input.OrganizationID,
		ProjectID:           input.ProjectID,
		WorkflowRunID:       input.WorkflowRunID,
		ShotID:              shots[1].ID,
		ShotIndex:           shots[1].ShotIndex,
		ShotNo:              shots[1].ShotNo,
		NodeRunID:           createOutput.NodeRunID,
		ProviderAsyncTaskID: createOutput.ProviderAsyncTaskID,
		ExternalTaskID:      createOutput.ExternalTaskID,
		Reason:              "user clicked cancel",
	})
	if err != nil {
		t.Fatalf("CancelShotVideoTask: %v", err)
	}
	if err := activities.CancelVideoProductionWorkflow(ctx, input, cancelOutput, "user clicked cancel"); err != nil {
		t.Fatalf("CancelVideoProductionWorkflow: %v", err)
	}
	if atomic.LoadInt32(&cancelCalls) != 1 {
		t.Fatalf("gateway cancel calls = %d, want 1", cancelCalls)
	}
	assertWorkflowRunStatus(t, ctx, pool, workflowRunID, "cancelled")
	assertWorkflowNodeStatus(t, ctx, pool, workflowRunID, "create_shot_video_1", "cancelled")
	assertStoryboardShotStatuses(t, ctx, pool, workflowRunID, []string{"video_succeeded", "cancelled", "cancelled"})
	for _, eventType := range []string{"workflow.run.cancelling", "workflow.node.cancelled", "workflow.run.cancelled", "provider.video.task.cancelled"} {
		assertWorkflowCancelEventType(t, ctx, pool, orgID, workflowRunID, eventType)
	}
}

func mockVideoWorkflowCancelGateway(t *testing.T, pool *pgxpool.Pool, textModelID, imageModelID, videoModelID string, cancelCalls *int32) http.Handler {
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
				Output: provider.GatewayTextOutput{Text: `{"title":"Cancel","shots":[
					{"shotNo":1,"duration":5,"visual":"station wide","imagePrompt":"station image 1","videoPrompt":"station video 1","camera":"push","motion":"mist","mood":"calm"},
					{"shotNo":2,"duration":5,"visual":"station middle","imagePrompt":"station image 2","videoPrompt":"station video 2","camera":"pan","motion":"steam","mood":"tense"},
					{"shotNo":3,"duration":5,"visual":"station close","imagePrompt":"station image 3","videoPrompt":"station video 3","camera":"tilt","motion":"light","mood":"quiet"}
				]}`},
			})
		case "/internal/provider/image/generate":
			var req provider.GatewayImageRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode image request: %v", err)
			}
			artifactID, mediaFileID, storageKey := insertMockGatewayMediaArtifact(t, r.Context(), pool, req.OrganizationID, req.ProjectID, req.WorkflowRunID, req.NodeRunID, imageModelID, "generated_image", "image/png", req.PromptTemplateKey, req.PromptVersionID, req.PromptHash, req.PromptSource)
			writeWorkflowGatewayEnvelope(t, w, provider.GatewayImageResponse{
				ProviderCallID: uuid.NewString(),
				ModelID:        imageModelID,
				Status:         "succeeded",
				Output: provider.GatewayImageOutput{
					ArtifactID:  artifactID,
					MediaFileID: mediaFileID,
					StorageKey:  storageKey,
					MimeType:    "image/png",
				},
			})
		case "/internal/provider/video/create-task":
			var req provider.GatewayVideoCreateTaskRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode create request: %v", err)
			}
			externalTaskID := "external-video-task-" + uuid.NewString()
			providerCallID, taskID := insertMockProviderAsyncTask(t, r.Context(), pool, req.OrganizationID, req.ProjectID, req.WorkflowRunID, req.NodeRunID, videoModelID, externalTaskID)
			writeWorkflowGatewayEnvelope(t, w, provider.GatewayVideoCreateTaskResponse{
				ProviderCallID:      providerCallID,
				ProviderAsyncTaskID: taskID,
				ExternalTaskID:      externalTaskID,
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

func assertStoryboardShotStatuses(t *testing.T, ctx context.Context, pool txQueryer, workflowRunID string, want []string) {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT status
		FROM storyboard_shots
		WHERE workflow_run_id = $1
		ORDER BY shot_index
	`, workflowRunID)
	if err != nil {
		t.Fatalf("select storyboard shot statuses: %v", err)
	}
	defer rows.Close()
	got := make([]string, 0)
	for rows.Next() {
		var status string
		if err := rows.Scan(&status); err != nil {
			t.Fatalf("scan storyboard shot status: %v", err)
		}
		got = append(got, status)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("shot statuses = %v, want %v", got, want)
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
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}
