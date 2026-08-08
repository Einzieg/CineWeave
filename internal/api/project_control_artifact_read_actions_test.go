package api

import (
	"encoding/json"
	"testing"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/controlmcp"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
)

func TestProjectControlArtifactReadsArePagedScopedAndPreviewable(t *testing.T) {
	_, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	firstID := seed.insertArtifact(t, "generated_image", "artifacts/first.png", "image/png")
	seed.insertArtifact(t, "generated_video", "artifacts/video.mp4", "video/mp4")
	seed.insertArtifact(t, "generated_image", "artifacts/second.png", "image/png")
	identity := controlmcp.Identity{
		Principal:      auth.Principal{UserID: seed.ownerUserID, OrganizationID: seed.organizationID},
		ControllerType: projectcontrol.ControllerCodexMCP,
		Key:            auth.ControlKeyMetadata{ID: "artifact-test-key"},
	}

	firstPage := executeProjectControlTestAction(t, seed, identity, "artifact.list", map[string]any{
		"projectId": seed.projectID, "type": "generated_image", "limit": 1,
	})
	var firstData artifactListActionPage
	if err := json.Unmarshal(firstPage.Data, &firstData); err != nil {
		t.Fatalf("decode first artifact page: %v", err)
	}
	if len(firstData.Items) != 1 || firstData.NextCursor == "" || firstPage.NextCursor != firstData.NextCursor {
		t.Fatalf("first artifact page=%+v resultCursor=%q", firstData, firstPage.NextCursor)
	}
	secondPage := executeProjectControlTestAction(t, seed, identity, "artifact.list", map[string]any{
		"projectId": seed.projectID, "type": "generated_image", "limit": 1, "cursor": firstData.NextCursor,
	})
	var secondData artifactListActionPage
	if err := json.Unmarshal(secondPage.Data, &secondData); err != nil {
		t.Fatalf("decode second artifact page: %v", err)
	}
	if len(secondData.Items) != 1 || secondData.NextCursor != "" || secondData.Items[0].ID == firstData.Items[0].ID {
		t.Fatalf("second artifact page=%+v", secondData)
	}

	get := executeProjectControlTestAction(t, seed, identity, "artifact.get", map[string]any{
		"projectId": seed.projectID, "artifactId": firstID,
	})
	var getData struct {
		Artifact Artifact `json:"artifact"`
	}
	if err := json.Unmarshal(get.Data, &getData); err != nil {
		t.Fatalf("decode artifact.get: %v", err)
	}
	if getData.Artifact.ID != firstID || getData.Artifact.ProjectID == nil || *getData.Artifact.ProjectID != seed.projectID {
		t.Fatalf("artifact.get=%+v", getData.Artifact)
	}

	preview := executeProjectControlTestAction(t, seed, identity, "artifact.preview_url", map[string]any{
		"projectId": seed.projectID, "artifactId": firstID, "expiresSeconds": 600,
	})
	var previewData artifactPreviewActionResult
	if err := json.Unmarshal(preview.Data, &previewData); err != nil {
		t.Fatalf("decode artifact.preview_url: %v", err)
	}
	if previewData.ArtifactID != firstID || previewData.URL == "" || previewData.StorageKey != "artifacts/first.png" || previewData.Method != "GET" {
		t.Fatalf("artifact preview=%+v", previewData)
	}
}
