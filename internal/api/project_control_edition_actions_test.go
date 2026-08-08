package api

import (
	"encoding/json"
	"net/http"
	"testing"

	editionpkg "github.com/Einzieg/cineweave/internal/edition"
	"github.com/Einzieg/cineweave/internal/projectcontrol"
)

func TestBuildEditionActionRequestBindsPathQueryAndBody(t *testing.T) {
	action := projectControlEditionAction{
		registration: editionpkg.ProjectControlActionRegistration{
			APIOperationID:  "testCommercialAction",
			QueryParameters: []string{"modelProfileKey"},
			Descriptor: projectcontrol.Descriptor{
				Name: "billing.test", Version: 1,
			},
		},
		api: editionpkg.APIModuleRegistration{
			Method:  http.MethodPost,
			Pattern: "/api/projects/{projectId}/billing-sponsorships/{sponsorshipId}/revoke",
		},
	}
	request, err := buildEditionActionRequest(
		t.Context(),
		Project{ID: "project-1"},
		action,
		json.RawMessage(`{
			"sponsorshipId":"sponsor/1",
			"modelProfileKey":"video.generate",
			"idempotencyKey":"command-1",
			"expectedRevision":4
		}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if request.PathValue("projectId") != "project-1" || request.PathValue("sponsorshipId") != "sponsor/1" {
		t.Fatalf("path values project=%q sponsorship=%q", request.PathValue("projectId"), request.PathValue("sponsorshipId"))
	}
	if request.URL.Query().Get("modelProfileKey") != "video.generate" {
		t.Fatalf("query=%q", request.URL.RawQuery)
	}
	var body map[string]any
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 1 || body["expectedRevision"] != float64(4) {
		t.Fatalf("body=%v", body)
	}
}
