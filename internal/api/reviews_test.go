package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Einzieg/cineweave/internal/auth"
	reviewpkg "github.com/Einzieg/cineweave/internal/review"
)

func TestDeterministicReviewChecks(t *testing.T) {
	t.Run("missing active script", func(t *testing.T) {
		_, seed := setupArtifactPreviewTest(t)
		defer seed.Close()

		items, err := reviewpkg.RunDeterministicProjectChecks(seed.ctx, seed.pool, seed.projectID)
		if err != nil {
			t.Fatalf("RunDeterministicProjectChecks: %v", err)
		}
		assertReviewIssue(t, items, "script", "high", "project")
	})

	t.Run("character missing primary reference", func(t *testing.T) {
		_, seed := setupArtifactPreviewTest(t)
		defer seed.Close()

		scriptID := seed.insertActiveScript(t)
		versionID := seed.currentScriptVersionID(t, scriptID)
		seed.insertScriptScene(t, scriptID, versionID, 1, "approved", "fresh")
		assetID := seed.insertCanonicalAsset(t, "character", "Lin Chu", "approved", "")

		items, err := reviewpkg.RunDeterministicProjectChecks(seed.ctx, seed.pool, seed.projectID)
		if err != nil {
			t.Fatalf("RunDeterministicProjectChecks: %v", err)
		}
		assertReviewIssue(t, items, "asset", "high", assetID)
	})

	t.Run("missing active final video", func(t *testing.T) {
		_, seed := setupArtifactPreviewTest(t)
		defer seed.Close()

		scriptID := seed.insertActiveScript(t)
		versionID := seed.currentScriptVersionID(t, scriptID)
		seed.insertScriptScene(t, scriptID, versionID, 1, "approved", "fresh")

		items, err := reviewpkg.RunDeterministicProjectChecks(seed.ctx, seed.pool, seed.projectID)
		if err != nil {
			t.Fatalf("RunDeterministicProjectChecks: %v", err)
		}
		assertReviewIssue(t, items, "final_video", "high", "project")
	})
}

func TestRunReviewWritesRunAndItems(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	var response RunProjectReviewResponse
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/reviews/run", seed.ownerToken, seed.organizationID, map[string]any{
		"reviewType":                 "project",
		"useAgent":                   false,
		"includeDeterministicChecks": true,
	}, &response)
	if response.ReviewRunID == "" || response.Status != "succeeded" || response.ItemCount == 0 {
		t.Fatalf("review response = %+v", response)
	}
	var runCount, itemCount int
	if err := seed.pool.QueryRow(seed.ctx, `SELECT count(*) FROM review_runs WHERE project_id = $1`, seed.projectID).Scan(&runCount); err != nil {
		t.Fatalf("count review runs: %v", err)
	}
	if err := seed.pool.QueryRow(seed.ctx, `SELECT count(*) FROM review_items WHERE project_id = $1 AND review_run_id = $2`, seed.projectID, response.ReviewRunID).Scan(&itemCount); err != nil {
		t.Fatalf("count review items: %v", err)
	}
	if runCount != 1 || itemCount != response.ItemCount {
		t.Fatalf("runCount=%d itemCount=%d response=%+v", runCount, itemCount, response)
	}
}

func TestReviewItemStatusTransitionsAndAccess(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	itemID := seed.insertReviewItem(t, "open", "asset", "high")
	assertAPIErrorCode(t, server, http.MethodGet, "/api/projects/"+seed.projectID+"/review-items", seed.otherToken, seed.organizationID, nil, http.StatusForbidden, "ACCESS_DENIED")

	var resolved ReviewItem
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/review-items/"+itemID+"/resolve", seed.ownerToken, seed.organizationID, map[string]any{"note": "fixed"}, &resolved)
	if resolved.Status != "resolved" || resolved.ResolvedBy == nil || resolved.ResolvedAt == nil || resolved.ResolutionNote == nil {
		t.Fatalf("resolved = %+v", resolved)
	}
	var ignored ReviewItem
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/review-items/"+itemID+"/ignore", seed.ownerToken, seed.organizationID, map[string]any{"note": "accepted"}, &ignored)
	if ignored.Status != "ignored" || ignored.ResolutionNote == nil {
		t.Fatalf("ignored = %+v", ignored)
	}
	var reopened ReviewItem
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/review-items/"+itemID+"/reopen", seed.ownerToken, seed.organizationID, map[string]any{}, &reopened)
	if reopened.Status != "open" || reopened.ResolvedBy != nil || reopened.ResolvedAt != nil || reopened.ResolutionNote != nil {
		t.Fatalf("reopened = %+v", reopened)
	}
}

func TestReviewRBACViewerCanReadEditorCanRun(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	viewer := seed.registerOrgMember(t, "review-viewer")
	editor := seed.registerOrgMember(t, "review-editor")
	owner := auth.TokenResponse{AccessToken: seed.ownerToken, OrganizationID: seed.organizationID}
	createUserRoleBinding(t, server, seed.pool, owner, viewer.User.ID, "project_viewer", "project", "", "", seed.projectID)
	createUserRoleBinding(t, server, seed.pool, owner, editor.User.ID, "project_editor", "project", "", "", seed.projectID)
	seed.insertReviewItem(t, "open", "script", "medium")

	var list struct {
		Items []ReviewItem `json:"items"`
	}
	doAPISuccess(t, server, http.MethodGet, "/api/projects/"+seed.projectID+"/review-items", viewer.AccessToken, seed.organizationID, nil, &list)
	if len(list.Items) == 0 {
		t.Fatal("viewer did not read review items")
	}
	var response RunProjectReviewResponse
	doAPISuccess(t, server, http.MethodPost, "/api/projects/"+seed.projectID+"/reviews/run", editor.AccessToken, seed.organizationID, map[string]any{"useAgent": false}, &response)
	if response.ReviewRunID == "" {
		t.Fatalf("editor run response = %+v", response)
	}
}

func assertReviewIssue(t *testing.T, items []reviewpkg.ReviewItemDraft, category, severity, entityID string) {
	t.Helper()
	for _, item := range items {
		if item.Category != category || item.Severity != severity {
			continue
		}
		if entityID == "project" && item.EntityType == "project" {
			return
		}
		if item.EntityID == entityID {
			return
		}
	}
	t.Fatalf("missing review issue category=%s severity=%s entityID=%s items=%+v", category, severity, entityID, items)
}

func (s *artifactPreviewSeed) insertReviewItem(t *testing.T, status, category, severity string) string {
	t.Helper()
	var id string
	if err := s.pool.QueryRow(s.ctx, `
		INSERT INTO review_items(
			organization_id, project_id, item_type, category, severity, title, description,
			entity_type, status, metadata, created_by
		)
		VALUES ($1, $2, 'issue', $3, $4, 'Review item', 'Description', 'project', $5, '{"actions":[]}', $6)
		RETURNING id::text
	`, s.organizationID, s.projectID, category, severity, status, s.ownerUserID).Scan(&id); err != nil {
		t.Fatalf("insert review item: %v", err)
	}
	return id
}

func (s *artifactPreviewSeed) registerOrgMember(t *testing.T, name string) auth.TokenResponse {
	t.Helper()
	resp, err := s.authService.Register(s.ctx, auth.RegisterRequest{
		Email:            name + "-" + randomStorageSegment() + "@example.test",
		Password:         "Password123!",
		DisplayName:      name,
		OrganizationName: name + " Org",
	}, httptest.NewRequest(http.MethodPost, "/api/auth/register", nil))
	if err != nil {
		t.Fatalf("register org member: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, resp.OrganizationID)
	})
	if _, err := s.pool.Exec(s.ctx, `INSERT INTO organization_members(organization_id, user_id, status) VALUES ($1, $2, 'active')`, s.organizationID, resp.User.ID); err != nil {
		t.Fatalf("insert organization member: %v", err)
	}
	return resp
}
