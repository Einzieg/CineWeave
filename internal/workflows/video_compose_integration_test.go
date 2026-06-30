package workflows

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Einzieg/cineweave/internal/db"
	"github.com/Einzieg/cineweave/internal/storage"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestVideoComposeIntegration(t *testing.T) {
	if os.Getenv("CINEWEAVE_INTEGRATION_TEST") != "1" {
		t.Skip("set CINEWEAVE_INTEGRATION_TEST=1 to run video compose integration test")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for video compose integration test")
	}

	ctx := context.Background()
	pool, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer pool.Close()
	storageClient, err := storage.New(ctx, storage.ConfigFromEnv())
	if err != nil {
		t.Fatalf("create storage client: %v", err)
	}

	orgID, userID, projectID, workflowRunID, _, _ := seedWorkflowGatewayIntegrationData(t, ctx, pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
	})

	tempDir := t.TempDir()
	clipPathA := filepath.Join(tempDir, "compose-a.mp4")
	clipPathB := filepath.Join(tempDir, "compose-b.mp4")
	writeComposeIntegrationClip(t, clipPathA, "testsrc=size=160x90:rate=24")
	writeComposeIntegrationClip(t, clipPathB, "testsrc2=size=160x90:rate=24")

	clipKeyA := fmt.Sprintf("test/%s/clip-a.mp4", workflowRunID)
	clipKeyB := fmt.Sprintf("test/%s/clip-b.mp4", workflowRunID)
	putA, err := storageClient.PutFile(ctx, clipKeyA, clipPathA, "video/mp4")
	if err != nil {
		t.Fatalf("upload clip A: %v", err)
	}
	putB, err := storageClient.PutFile(ctx, clipKeyB, clipPathB, "video/mp4")
	if err != nil {
		t.Fatalf("upload clip B: %v", err)
	}
	artifactA, mediaA := insertVideoComposeClip(t, ctx, pool, orgID, projectID, workflowRunID, userID, 0, putA)
	artifactB, mediaB := insertVideoComposeClip(t, ctx, pool, orgID, projectID, workflowRunID, userID, 1, putB)
	if artifactA == "" || artifactB == "" || mediaA == "" || mediaB == "" {
		t.Fatal("compose clip seed did not return artifact/media ids")
	}

	activities := NewActivities(pool, storageClient, nil)
	output, err := activities.ComposeFinalVideo(ctx, ComposeFinalVideoInput{
		OrganizationID: orgID,
		ProjectID:      projectID,
		WorkflowRunID:  workflowRunID,
		CreatedBy:      userID,
		AspectRatio:    "16:9",
		Resolution:     "720p",
	})
	if err != nil {
		t.Fatalf("ComposeFinalVideo: %v", err)
	}
	if output.ArtifactID == "" || output.MediaFileID == "" || output.StorageKey == "" || output.TimelineArtifactID == "" || output.MimeType != "video/mp4" {
		t.Fatalf("compose output = %+v", output)
	}
	assertVideoComposeRows(t, ctx, pool, workflowRunID, output)
	if _, _, err := storageClient.GetObject(ctx, output.StorageKey, 32<<20); err != nil {
		t.Fatalf("download final video: %v", err)
	}
	presigned, err := storageClient.PresignGetObject(ctx, output.StorageKey, time.Minute)
	if err != nil {
		t.Fatalf("presign final video: %v", err)
	}
	if presigned.URL == "" || presigned.Method != "GET" {
		t.Fatalf("presigned = %+v", presigned)
	}
}

func insertVideoComposeClip(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, projectID, workflowRunID, userID string, shotIndex int, put storage.PutResult) (string, string) {
	t.Helper()
	var artifactID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO artifacts(organization_id, project_id, workflow_run_id, type, storage_key, mime_type, content_hash, metadata, created_by)
		VALUES ($1, $2, $3, 'generated_video', $4, 'video/mp4', $5, $6, $7)
		RETURNING id
	`, orgID, projectID, workflowRunID, put.StorageKey, put.ContentHash, mustJSON(map[string]any{"source": "video_compose_integration"}), userID).Scan(&artifactID); err != nil {
		t.Fatalf("insert clip artifact: %v", err)
	}
	var mediaFileID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO media_files(organization_id, project_id, artifact_id, storage_key, mime_type, byte_size, checksum, duration_seconds, metadata, created_by)
		VALUES ($1, $2, $3, $4, 'video/mp4', $5, $6, 0.25, $7, $8)
		RETURNING id
	`, orgID, projectID, artifactID, put.StorageKey, put.ByteSize, put.ContentHash, mustJSON(map[string]any{"source": "video_compose_integration"}), userID).Scan(&mediaFileID); err != nil {
		t.Fatalf("insert clip media file: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO storyboard_shots(
			organization_id, project_id, workflow_run_id, shot_index, shot_no, duration_seconds,
			visual, image_prompt, video_prompt,
			video_artifact_id, video_media_file_id, video_storage_key, status, metadata
		)
		VALUES ($1, $2, $3, $4, $5, 0.25, $6, $6, $6, $7, $8, $9, 'video_succeeded', $10)
	`, orgID, projectID, workflowRunID, shotIndex, shotIndex+1, fmt.Sprintf("compose shot %d", shotIndex+1), artifactID, mediaFileID, put.StorageKey, mustJSON(map[string]any{"source": "video_compose_integration"})); err != nil {
		t.Fatalf("insert storyboard shot: %v", err)
	}
	return artifactID, mediaFileID
}

func assertVideoComposeRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, workflowRunID string, output ComposeFinalVideoOutput) {
	t.Helper()
	var finalType, finalStorageKey, finalMediaFileID string
	if err := pool.QueryRow(ctx, `
		SELECT a.type, a.storage_key, COALESCE(a.metadata->>'mediaFileId', '')
		FROM artifacts a
		WHERE a.id = $1 AND a.workflow_run_id = $2
	`, output.ArtifactID, workflowRunID).Scan(&finalType, &finalStorageKey, &finalMediaFileID); err != nil {
		t.Fatalf("select final artifact: %v", err)
	}
	if finalType != "final_video" || finalStorageKey != output.StorageKey || finalMediaFileID != output.MediaFileID {
		t.Fatalf("final artifact = %s/%s/%s", finalType, finalStorageKey, finalMediaFileID)
	}
	var timelineType string
	if err := pool.QueryRow(ctx, `SELECT type FROM artifacts WHERE id = $1 AND workflow_run_id = $2`, output.TimelineArtifactID, workflowRunID).Scan(&timelineType); err != nil {
		t.Fatalf("select timeline artifact: %v", err)
	}
	if timelineType != "timeline_json" {
		t.Fatalf("timeline type = %q", timelineType)
	}
	var mediaStorageKey string
	if err := pool.QueryRow(ctx, `SELECT storage_key FROM media_files WHERE id = $1 AND artifact_id = $2`, output.MediaFileID, output.ArtifactID).Scan(&mediaStorageKey); err != nil {
		t.Fatalf("select media file: %v", err)
	}
	if mediaStorageKey != output.StorageKey {
		t.Fatalf("media storage key = %q, want %q", mediaStorageKey, output.StorageKey)
	}
	var nodeStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM workflow_node_runs WHERE workflow_run_id = $1 AND node_key = $2`, workflowRunID, nodeComposeFinalVideoKey).Scan(&nodeStatus); err != nil {
		t.Fatalf("select compose node: %v", err)
	}
	if nodeStatus != "succeeded" {
		t.Fatalf("compose node status = %q", nodeStatus)
	}
	for _, eventType := range []string{"workflow.node.completed", "artifact.created", "media.compose.completed"} {
		var count int
		if err := pool.QueryRow(ctx, `
			SELECT count(*)
			FROM event_outbox
			WHERE event_type = $1
			  AND (payload->>'workflowRunId' = $2 OR aggregate_id = $2::uuid)
		`, eventType, workflowRunID).Scan(&count); err != nil {
			t.Fatalf("select event %s: %v", eventType, err)
		}
		if count == 0 {
			t.Fatalf("missing event %s", eventType)
		}
	}
}

func writeComposeIntegrationClip(t *testing.T, outputPath, source string) {
	t.Helper()
	cmd := exec.Command(
		"ffmpeg",
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-f", "lavfi",
		"-i", source,
		"-t", "0.25",
		"-pix_fmt", "yuv420p",
		outputPath,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create integration clip: %v: %s", err, strings.TrimSpace(string(output)))
	}
}
