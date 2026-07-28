package api

import (
	"net/http"
	"net/url"
	"testing"
)

type workflowRunPage struct {
	Items      []WorkflowRun `json:"items"`
	NextCursor string        `json:"nextCursor"`
	HasMore    bool          `json:"hasMore"`
}

func TestWorkflowActivityPaginationAndClear(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	for range 3 {
		seed.insertWorkflowRun(t, "succeeded")
	}
	runningID := seed.insertWorkflowRun(t, "running")

	var firstPage workflowRunPage
	doAPISuccess(
		t,
		server,
		http.MethodGet,
		"/api/workflow-runs?filter%5BprojectId%5D="+seed.projectID+"&filter%5Bstatus%5D=terminal&view=activity&limit=2",
		seed.ownerToken,
		seed.organizationID,
		nil,
		&firstPage,
	)
	if len(firstPage.Items) != 2 || !firstPage.HasMore || firstPage.NextCursor == "" {
		t.Fatalf("first page = %+v, want 2 items with next cursor", firstPage)
	}

	var secondPage workflowRunPage
	doAPISuccess(
		t,
		server,
		http.MethodGet,
		"/api/workflow-runs?filter%5BprojectId%5D="+seed.projectID+"&filter%5Bstatus%5D=terminal&view=activity&limit=2&cursor="+url.QueryEscape(firstPage.NextCursor),
		seed.ownerToken,
		seed.organizationID,
		nil,
		&secondPage,
	)
	if len(secondPage.Items) != 1 || secondPage.HasMore || secondPage.NextCursor != "" {
		t.Fatalf("second page = %+v, want final item", secondPage)
	}

	var cleared struct {
		ClearedCount   int    `json:"clearedCount"`
		ClearedThrough string `json:"clearedThrough"`
	}
	doAPISuccess(
		t,
		server,
		http.MethodPost,
		"/api/projects/"+seed.projectID+"/workflow-activity/clear-completed",
		seed.ownerToken,
		seed.organizationID,
		nil,
		&cleared,
	)
	if cleared.ClearedCount != 3 || cleared.ClearedThrough == "" {
		t.Fatalf("clear result = %+v, want 3 terminal runs", cleared)
	}

	var hidden workflowRunPage
	doAPISuccess(
		t,
		server,
		http.MethodGet,
		"/api/workflow-runs?filter%5BprojectId%5D="+seed.projectID+"&filter%5Bstatus%5D=terminal&view=activity&limit=20",
		seed.ownerToken,
		seed.organizationID,
		nil,
		&hidden,
	)
	if len(hidden.Items) != 0 {
		t.Fatalf("activity terminal items after clear = %d, want 0", len(hidden.Items))
	}

	var active workflowRunPage
	doAPISuccess(
		t,
		server,
		http.MethodGet,
		"/api/workflow-runs?filter%5BprojectId%5D="+seed.projectID+"&filter%5Bstatus%5D=active&view=activity&limit=20",
		seed.ownerToken,
		seed.organizationID,
		nil,
		&active,
	)
	if len(active.Items) != 1 || active.Items[0].ID != runningID {
		t.Fatalf("active items after clear = %+v, want running workflow %s", active.Items, runningID)
	}

	var history workflowRunPage
	doAPISuccess(
		t,
		server,
		http.MethodGet,
		"/api/workflow-runs?filter%5BprojectId%5D="+seed.projectID+"&filter%5Bstatus%5D=terminal&limit=20",
		seed.ownerToken,
		seed.organizationID,
		nil,
		&history,
	)
	if len(history.Items) != 3 {
		t.Fatalf("terminal history items after clear = %d, want 3", len(history.Items))
	}
}

func TestWorkflowRunListRejectsInvalidCursor(t *testing.T) {
	server, seed := setupArtifactPreviewTest(t)
	defer seed.Close()

	assertAPIErrorCode(
		t,
		server,
		http.MethodGet,
		"/api/workflow-runs?filter%5BprojectId%5D="+seed.projectID+"&cursor=invalid",
		seed.ownerToken,
		seed.organizationID,
		nil,
		http.StatusUnprocessableEntity,
		"VALIDATION_FAILED",
	)
}
