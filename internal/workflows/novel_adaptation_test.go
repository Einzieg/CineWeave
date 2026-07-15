package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
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
	env.RegisterWorkflow(GenerateSourceScriptEpisodeWorkflow)
	env.RegisterActivityWithOptions(func(ctx context.Context, input PrepareScriptFromSourceInput) (SourceToScriptPlan, error) {
		if input.SourceID != "source-1" || input.Instruction != "keep it visual" || input.IdempotencyKey != "step-1" {
			t.Fatalf("input = %+v", input)
		}
		return SourceToScriptPlan{
			SourceID:        input.SourceID,
			SourceType:      "novel",
			SourceTitle:     "source",
			ScriptID:        "script-1",
			ScriptVersionID: "version-1",
			EpisodeTotal:    2,
			Chapters: []SourceToScriptChapterRef{
				{ID: "chapter-1", ChapterIndex: 1, Title: "第1节"},
				{ID: "chapter-2", ChapterIndex: 2, Title: "第2节"},
			},
		}, nil
	}, activity.RegisterOptions{Name: "PrepareScriptFromSource"})
	env.RegisterActivityWithOptions(func(ctx context.Context, input GenerateSourceScriptEpisodeInput) (SourceScriptEpisodeOutput, error) {
		if input.ScriptVersionID != "version-1" || input.EpisodeTotal != 2 || input.Chapter.ID == "" {
			t.Fatalf("episode input = %+v", input)
		}
		return SourceScriptEpisodeOutput{
			SourceID:        input.SourceID,
			SourceChapterID: input.Chapter.ID,
			ScriptID:        input.ScriptID,
			ScriptVersionID: input.ScriptVersionID,
			EpisodeID:       "episode-" + input.Chapter.ID,
			EpisodeIndex:    input.EpisodeIndex,
			Content:         "script " + input.Chapter.ID,
		}, nil
	}, activity.RegisterOptions{Name: "GenerateSourceScriptEpisode"})
	env.RegisterActivityWithOptions(func(ctx context.Context, input GenerateScriptFromSourceInput, plan SourceToScriptPlan, finalization SourceToScriptFinalization) (SourceToScriptOutput, error) {
		if plan.ScriptID != "script-1" || finalization.CompletedEpisodeCount != 2 || finalization.FailedEpisodeCount != 0 {
			t.Fatalf("finalize plan=%+v finalization=%+v", plan, finalization)
		}
		return SourceToScriptOutput{
			Status:          "succeeded",
			SourceID:        input.SourceID,
			ScriptID:        plan.ScriptID,
			ScriptVersionID: plan.ScriptVersionID,
			EpisodeCount:    finalization.CompletedEpisodeCount,
			TotalItems:      finalization.RequestedEpisodeCount,
			CompletedItems:  finalization.CompletedEpisodeCount,
			Content:         "script",
		}, nil
	}, activity.RegisterOptions{Name: "FinalizeScriptFromSource"})
	env.RegisterActivityWithOptions(func(ctx context.Context, input TextToStoryboardInput, output SourceToScriptOutput) error {
		if output.EpisodeCount != 2 {
			t.Fatalf("output = %+v", output)
		}
		return nil
	}, activity.RegisterOptions{Name: "CompleteSourceToScriptWorkflow"})
	env.ExecuteWorkflow(SourceToScriptWorkflow, TextToStoryboardInput{
		OrganizationID: "org-1",
		ProjectID:      "project-1",
		WorkflowRunID:  "workflow-1",
		CreatedBy:      "user-1",
		Input:          json.RawMessage(`{"sourceId":"source-1","instruction":"keep it visual","idempotencyKey":"step-1","chapterIds":["chapter-1","chapter-2"]}`),
	})
	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("workflow error = %v", env.GetWorkflowError())
	}
	var output SourceToScriptOutput
	if err := env.GetWorkflowResult(&output); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if output.EpisodeCount != 2 || output.ScriptID != "script-1" {
		t.Fatalf("output = %+v", output)
	}
}

func TestSourceToScriptWorkflowContinuesAfterIndependentEpisodeFailure(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(SourceToScriptWorkflow)
	env.RegisterWorkflow(GenerateSourceScriptEpisodeWorkflow)
	env.RegisterActivityWithOptions(func(context.Context, PrepareScriptFromSourceInput) (SourceToScriptPlan, error) {
		return SourceToScriptPlan{
			SourceID: "source-1", SourceType: "novel", ScriptID: "script-1", ScriptVersionID: "version-1", EpisodeTotal: 3,
			Chapters: []SourceToScriptChapterRef{{ID: "chapter-1"}, {ID: "chapter-2"}, {ID: "chapter-3"}},
		}, nil
	}, activity.RegisterOptions{Name: "PrepareScriptFromSource"})
	var mu sync.Mutex
	seen := map[int]int{}
	env.RegisterActivityWithOptions(func(_ context.Context, input GenerateSourceScriptEpisodeInput) (SourceScriptEpisodeOutput, error) {
		mu.Lock()
		seen[input.EpisodeIndex]++
		mu.Unlock()
		if input.EpisodeIndex == 2 {
			return SourceScriptEpisodeOutput{}, temporal.NewNonRetryableApplicationError("episode rejected", "CONTENT_REJECTED", nil)
		}
		return SourceScriptEpisodeOutput{EpisodeID: fmt.Sprintf("episode-%d", input.EpisodeIndex), EpisodeIndex: input.EpisodeIndex}, nil
	}, activity.RegisterOptions{Name: "GenerateSourceScriptEpisode"})
	env.RegisterActivityWithOptions(func(_ context.Context, input FailSourceScriptEpisodeInput) error {
		if input.Episode.EpisodeIndex != 2 || input.ErrorCode != "CONTENT_REJECTED" {
			t.Fatalf("failure finalizer input = %+v", input)
		}
		return nil
	}, activity.RegisterOptions{Name: "FailSourceScriptEpisode"})
	env.RegisterActivityWithOptions(func(_ context.Context, _ GenerateScriptFromSourceInput, _ SourceToScriptPlan, finalization SourceToScriptFinalization) (SourceToScriptOutput, error) {
		if finalization.RequestedEpisodeCount != 3 || finalization.CompletedEpisodeCount != 2 || finalization.FailedEpisodeCount != 1 {
			t.Fatalf("finalization = %+v", finalization)
		}
		return SourceToScriptOutput{Status: "partial_succeeded", EpisodeCount: 2, TotalItems: 3, CompletedItems: 2, FailedItems: 1, FailedEpisodes: []int{2}}, nil
	}, activity.RegisterOptions{Name: "FinalizeScriptFromSource"})
	env.RegisterActivityWithOptions(func(_ context.Context, _ TextToStoryboardInput, output SourceToScriptOutput) error {
		if output.Status != "partial_succeeded" || output.FailedItems != 1 {
			t.Fatalf("completion output = %+v", output)
		}
		return nil
	}, activity.RegisterOptions{Name: "CompleteSourceToScriptWorkflow"})
	env.ExecuteWorkflow(SourceToScriptWorkflow, TextToStoryboardInput{
		OrganizationID: "org-1", ProjectID: "project-1", WorkflowRunID: "workflow-1", CreatedBy: "user-1",
		Input: json.RawMessage(`{"sourceId":"source-1","maxConcurrency":2}`),
	})
	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("workflow error = %v", env.GetWorkflowError())
	}
	mu.Lock()
	defer mu.Unlock()
	for episodeIndex := 1; episodeIndex <= 3; episodeIndex++ {
		if seen[episodeIndex] != 1 {
			t.Fatalf("episode %d attempts = %d, want 1", episodeIndex, seen[episodeIndex])
		}
	}
}

func TestSourceToScriptWorkflowContinueAsNewCarriesCompactStableCheckpoint(t *testing.T) {
	chapters := make([]SourceToScriptChapterRef, 0, defaultSourceEpisodesPerRun+5)
	chapterIDs := make([]string, 0, cap(chapters))
	for index := 0; index < cap(chapters); index++ {
		chapterID := fmt.Sprintf("chapter-%02d", index+1)
		chapterIDs = append(chapterIDs, chapterID)
		chapters = append(chapters, SourceToScriptChapterRef{ID: chapterID, ChapterIndex: index + 1, Title: fmt.Sprintf("第%d节", index+1)})
	}
	raw, err := json.Marshal(SourceToScriptOptions{
		SourceID: "source-1", ChapterIDs: chapterIDs, Instruction: "faithful", MaxConcurrency: 5, IdempotencyKey: "logical-task-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	initial := TextToStoryboardInput{
		OrganizationID: "org-1", ProjectID: "project-1", WorkflowRunID: "database-workflow-run-1", CreatedBy: "user-1", Input: raw,
	}

	var suite testsuite.WorkflowTestSuite
	firstRun := suite.NewTestWorkflowEnvironment()
	firstRun.RegisterWorkflow(SourceToScriptWorkflow)
	firstRun.RegisterWorkflow(GenerateSourceScriptEpisodeWorkflow)
	firstRun.RegisterActivityWithOptions(func(context.Context, PrepareScriptFromSourceInput) (SourceToScriptPlan, error) {
		return SourceToScriptPlan{
			SourceID: "source-1", SourceType: "novel", ScriptID: "script-1", ScriptVersionID: "version-1",
			EpisodeTotal: len(chapters), Chapters: chapters,
		}, nil
	}, activity.RegisterOptions{Name: "PrepareScriptFromSource"})
	firstRun.RegisterActivityWithOptions(func(_ context.Context, input GenerateSourceScriptEpisodeInput) (SourceScriptEpisodeOutput, error) {
		if input.WorkflowRunID != initial.WorkflowRunID || input.EpisodeIndex < 1 || input.EpisodeIndex > defaultSourceEpisodesPerRun {
			t.Fatalf("first-run episode identity = %+v", input)
		}
		return SourceScriptEpisodeOutput{EpisodeID: fmt.Sprintf("episode-%02d", input.EpisodeIndex), EpisodeIndex: input.EpisodeIndex}, nil
	}, activity.RegisterOptions{Name: "GenerateSourceScriptEpisode"})
	firstRun.ExecuteWorkflow(SourceToScriptWorkflow, initial)
	if !firstRun.IsWorkflowCompleted() || !workflow.IsContinueAsNewError(firstRun.GetWorkflowError()) {
		t.Fatalf("first run error = %v, want ContinueAsNew", firstRun.GetWorkflowError())
	}
	var continueErr *workflow.ContinueAsNewError
	if !errors.As(firstRun.GetWorkflowError(), &continueErr) {
		t.Fatalf("continue error type = %T", firstRun.GetWorkflowError())
	}
	var continued TextToStoryboardInput
	if err := converter.GetDefaultDataConverter().FromPayloads(continueErr.Input, &continued); err != nil {
		t.Fatalf("decode ContinueAsNew input: %v", err)
	}
	if continued.WorkflowRunID != initial.WorkflowRunID || string(continued.Input) != string(initial.Input) || continued.SourceToScriptState == nil {
		t.Fatalf("continued identity = %+v", continued)
	}
	checkpoint := continued.SourceToScriptState
	if !checkpoint.Initialized || checkpoint.NextEpisodeIndex != defaultSourceEpisodesPerRun || checkpoint.CompletedEpisodeCount != defaultSourceEpisodesPerRun || checkpoint.ContinueCount != 1 || checkpoint.Plan.ScriptVersionID != "version-1" {
		t.Fatalf("continued checkpoint = %+v", checkpoint)
	}

	secondRun := suite.NewTestWorkflowEnvironment()
	secondRun.RegisterWorkflow(SourceToScriptWorkflow)
	secondRun.RegisterWorkflow(GenerateSourceScriptEpisodeWorkflow)
	secondRun.RegisterActivityWithOptions(func(_ context.Context, input GenerateSourceScriptEpisodeInput) (SourceScriptEpisodeOutput, error) {
		if input.WorkflowRunID != initial.WorkflowRunID || input.EpisodeIndex <= defaultSourceEpisodesPerRun {
			t.Fatalf("second-run episode identity = %+v", input)
		}
		return SourceScriptEpisodeOutput{EpisodeID: fmt.Sprintf("episode-%02d", input.EpisodeIndex), EpisodeIndex: input.EpisodeIndex}, nil
	}, activity.RegisterOptions{Name: "GenerateSourceScriptEpisode"})
	secondRun.RegisterActivityWithOptions(func(_ context.Context, input GenerateScriptFromSourceInput, plan SourceToScriptPlan, finalization SourceToScriptFinalization) (SourceToScriptOutput, error) {
		if input.WorkflowRunID != initial.WorkflowRunID || finalization.CompletedEpisodeCount != len(chapters) || finalization.FailedEpisodeCount != 0 || plan.ScriptVersionID != "version-1" {
			t.Fatalf("final checkpoint input=%+v plan=%+v finalization=%+v", input, plan, finalization)
		}
		return SourceToScriptOutput{Status: "succeeded", ScriptID: plan.ScriptID, ScriptVersionID: plan.ScriptVersionID, EpisodeCount: finalization.CompletedEpisodeCount, TotalItems: finalization.RequestedEpisodeCount, CompletedItems: finalization.CompletedEpisodeCount}, nil
	}, activity.RegisterOptions{Name: "FinalizeScriptFromSource"})
	secondRun.RegisterActivityWithOptions(func(_ context.Context, input TextToStoryboardInput, output SourceToScriptOutput) error {
		if input.WorkflowRunID != initial.WorkflowRunID || output.EpisodeCount != len(chapters) {
			t.Fatalf("completion input=%+v output=%+v", input, output)
		}
		return nil
	}, activity.RegisterOptions{Name: "CompleteSourceToScriptWorkflow"})
	secondRun.ExecuteWorkflow(SourceToScriptWorkflow, continued)
	if !secondRun.IsWorkflowCompleted() || secondRun.GetWorkflowError() != nil {
		t.Fatalf("second run error = %v", secondRun.GetWorkflowError())
	}
}
