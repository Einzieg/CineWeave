package workflows

import (
	"context"
	"encoding/json"
	"testing"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func TestNormalizeNovelEventExtraction(t *testing.T) {
	extraction, err := NormalizeNovelEventExtraction("```json\n" + `{
	  "events": [
	    {
	      "title": "Opening",
	      "summary": "The protagonist finds the old camera.",
	      "eventType": "reveal",
	      "importance": 9,
	      "characters": [" Lin ", "Lin"],
	      "scenes": ["Station"],
	      "props": ["Camera"],
	      "keywords": ["arrival"],
	      "rawExcerpt": "A camera waits on the bench."
	    },
	    {
	      "summary": "A train arrives.",
	      "importance": 0
	    }
	  ],
	  "links": [
	    {"sourceEventIndex": 1, "targetEventIndex": 2, "linkType": "causes"},
	    {"sourceEventIndex": 2, "targetEventIndex": 99, "linkType": "invalid"}
	  ]
	}` + "\n```")
	if err != nil {
		t.Fatalf("NormalizeNovelEventExtraction: %v", err)
	}
	if len(extraction.Events) != 2 {
		t.Fatalf("events len = %d, want 2", len(extraction.Events))
	}
	if extraction.Events[0].Importance != 5 || extraction.Events[1].Importance != 3 {
		t.Fatalf("importance = %d/%d", extraction.Events[0].Importance, extraction.Events[1].Importance)
	}
	if extraction.Events[1].Title == "" || extraction.Events[1].Summary == "" {
		t.Fatalf("event defaults not applied: %+v", extraction.Events[1])
	}
	if len(extraction.Events[0].Characters) != 1 || extraction.Events[0].Characters[0] != "Lin" {
		t.Fatalf("characters = %+v", extraction.Events[0].Characters)
	}
	if len(extraction.Links) != 1 || extraction.Links[0].LinkType != "causes" {
		t.Fatalf("links = %+v", extraction.Links)
	}
}

func TestNormalizeAdaptationPlan(t *testing.T) {
	events := []NovelEventRecord{
		{ID: "event-a", EventIndex: 1, SequenceNo: 1001, Title: "Opening"},
		{ID: "event-b", EventIndex: 2, SequenceNo: 1002, Title: "Conflict"},
	}
	plan, err := NormalizeAdaptationPlan(`{
	  "title": "Pilot",
	  "logline": "A concise story.",
	  "structure": {"opening": "Camera"},
	  "selectedEvents": ["1", "event-b", "Missing"],
	  "omittedEvents": [{"event": "Other", "reason": "Too long"}],
	  "visualStrategy": "Wide frames",
	  "estimatedShots": 3
	}`, events)
	if err != nil {
		t.Fatalf("NormalizeAdaptationPlan: %v", err)
	}
	if plan.Title != "Pilot" || len(plan.SelectedEvents) != 2 {
		t.Fatalf("plan = %+v", plan)
	}
	if plan.SelectedEvents[0] != "event-a" || plan.SelectedEvents[1] != "event-b" {
		t.Fatalf("selected events = %+v", plan.SelectedEvents)
	}
	if !json.Valid(plan.Structure) || !json.Valid(plan.Raw) {
		t.Fatalf("invalid JSON structure/raw")
	}
}

func TestSourceToScriptWorkflowReturnsNovelPlanOutput(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(SourceToScriptWorkflow)
	env.RegisterActivityWithOptions(func(ctx context.Context, input GenerateScriptFromSourceInput) (SourceToScriptOutput, error) {
		if input.SourceID != "source-1" || input.Instruction != "keep it visual" {
			t.Fatalf("input = %+v", input)
		}
		return SourceToScriptOutput{
			SourceID:         input.SourceID,
			AdaptationPlanID: "plan-1",
			ScriptID:         "script-1",
			ScriptVersionID:  "version-1",
			Content:          "script",
		}, nil
	}, activity.RegisterOptions{Name: "GenerateScriptFromSource"})
	env.RegisterActivityWithOptions(func(ctx context.Context, input TextToStoryboardInput, output SourceToScriptOutput) error {
		if output.AdaptationPlanID != "plan-1" {
			t.Fatalf("output = %+v", output)
		}
		return nil
	}, activity.RegisterOptions{Name: "CompleteSourceToScriptWorkflow"})
	env.ExecuteWorkflow(SourceToScriptWorkflow, TextToStoryboardInput{
		OrganizationID: "org-1",
		ProjectID:      "project-1",
		WorkflowRunID:  "workflow-1",
		CreatedBy:      "user-1",
		Input:          json.RawMessage(`{"sourceId":"source-1","instruction":"keep it visual"}`),
	})
	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("workflow error = %v", env.GetWorkflowError())
	}
	var output SourceToScriptOutput
	if err := env.GetWorkflowResult(&output); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if output.AdaptationPlanID != "plan-1" || output.ScriptID != "script-1" {
		t.Fatalf("output = %+v", output)
	}
}
