package workflows

import (
	"testing"
	"time"

	commonpb "go.temporal.io/api/common/v1"
	enumspb "go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	taskqueuepb "go.temporal.io/api/taskqueue/v1"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/worker"
	"google.golang.org/protobuf/types/known/durationpb"
)

func TestEpisodeVideoProductionV1HistoryReplaysAfterV2Deployment(t *testing.T) {
	input := EpisodeVideoProductionInput{
		Plan: EpisodeVideoProductionPlan{
			CheckpointID: "checkpoint-v1", OrganizationID: "organization-v1", ProjectID: "project-v1",
			WorkflowRunID: "workflow-run-v1", TemporalWorkflowID: "episode-video-v1",
		},
	}
	output := BatchShotProductionOutput{
		Action: "batch_generate_shot_videos", Status: "succeeded", WorkflowRunID: input.Plan.WorkflowRunID,
		TargetShotIDs: []string{}, SucceededShotIDs: []string{}, FailedShotIDs: []string{},
		ProviderAsyncTaskIDs: map[string]string{}, Errors: map[string]string{}, ErrorCodes: map[string]string{},
	}
	batch := EpisodeVideoProductionBatch{Done: true, FinalOutput: output}

	dataConverter := converter.GetDefaultDataConverter()
	inputPayloads, err := dataConverter.ToPayloads(input)
	if err != nil {
		t.Fatalf("encode v1 workflow input: %v", err)
	}
	activityInputPayloads, err := dataConverter.ToPayloads(input)
	if err != nil {
		t.Fatalf("encode v1 activity input: %v", err)
	}
	activityResultPayloads, err := dataConverter.ToPayloads(batch)
	if err != nil {
		t.Fatalf("encode v1 activity result: %v", err)
	}
	workflowResultPayloads, err := dataConverter.ToPayloads(output)
	if err != nil {
		t.Fatalf("encode v1 workflow result: %v", err)
	}

	const taskQueue = "episode-video-v1-replay"
	history := &historypb.History{Events: []*historypb.HistoryEvent{
		episodeVideoWorkflowStartedEvent(1, &historypb.WorkflowExecutionStartedEventAttributes{
			WorkflowType: &commonpb.WorkflowType{Name: "EpisodeVideoProductionWorkflow"},
			TaskQueue:    &taskqueuepb.TaskQueue{Name: taskQueue}, Input: inputPayloads,
			WorkflowTaskTimeout: durationpb.New(10 * time.Second), Attempt: 1,
		}),
		episodeVideoWorkflowTaskScheduledEvent(2, &historypb.WorkflowTaskScheduledEventAttributes{
			TaskQueue: &taskqueuepb.TaskQueue{Name: taskQueue}, StartToCloseTimeout: durationpb.New(10 * time.Second), Attempt: 1,
		}),
		episodeVideoWorkflowTaskStartedEvent(3, &historypb.WorkflowTaskStartedEventAttributes{ScheduledEventId: 2}),
		episodeVideoWorkflowTaskCompletedEvent(4, &historypb.WorkflowTaskCompletedEventAttributes{ScheduledEventId: 2, StartedEventId: 3}),
		episodeVideoActivityTaskScheduledEvent(5, &historypb.ActivityTaskScheduledEventAttributes{
			ActivityId: "5", ActivityType: &commonpb.ActivityType{Name: "PrepareEpisodeVideoProductionBatch"},
			TaskQueue: &taskqueuepb.TaskQueue{Name: taskQueue}, Input: activityInputPayloads,
			WorkflowTaskCompletedEventId: 4, StartToCloseTimeout: durationpb.New(2 * time.Minute),
			RetryPolicy: &commonpb.RetryPolicy{InitialInterval: durationpb.New(time.Second), BackoffCoefficient: 2, MaximumAttempts: 3},
		}),
		episodeVideoActivityTaskStartedEvent(6, &historypb.ActivityTaskStartedEventAttributes{ScheduledEventId: 5}),
		episodeVideoActivityTaskCompletedEvent(7, &historypb.ActivityTaskCompletedEventAttributes{
			ScheduledEventId: 5, StartedEventId: 6, Result: activityResultPayloads,
		}),
		episodeVideoWorkflowTaskScheduledEvent(8, &historypb.WorkflowTaskScheduledEventAttributes{
			TaskQueue: &taskqueuepb.TaskQueue{Name: taskQueue}, StartToCloseTimeout: durationpb.New(10 * time.Second), Attempt: 1,
		}),
		episodeVideoWorkflowTaskStartedEvent(9, &historypb.WorkflowTaskStartedEventAttributes{ScheduledEventId: 8}),
		episodeVideoWorkflowTaskCompletedEvent(10, &historypb.WorkflowTaskCompletedEventAttributes{ScheduledEventId: 8, StartedEventId: 9}),
		episodeVideoWorkflowCompletedEvent(11, &historypb.WorkflowExecutionCompletedEventAttributes{
			Result: workflowResultPayloads, WorkflowTaskCompletedEventId: 10,
		}),
	}}

	replayer := worker.NewWorkflowReplayer()
	replayer.RegisterWorkflow(EpisodeVideoProductionWorkflow)
	if err := replayer.ReplayWorkflowHistory(nil, history); err != nil {
		t.Fatalf("replay v1 episode video history after v2 deployment: %v", err)
	}
}

func episodeVideoWorkflowStartedEvent(eventID int64, attributes *historypb.WorkflowExecutionStartedEventAttributes) *historypb.HistoryEvent {
	return &historypb.HistoryEvent{EventId: eventID, EventType: enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_STARTED,
		Attributes: &historypb.HistoryEvent_WorkflowExecutionStartedEventAttributes{WorkflowExecutionStartedEventAttributes: attributes}}
}

func episodeVideoWorkflowTaskScheduledEvent(eventID int64, attributes *historypb.WorkflowTaskScheduledEventAttributes) *historypb.HistoryEvent {
	return &historypb.HistoryEvent{EventId: eventID, EventType: enumspb.EVENT_TYPE_WORKFLOW_TASK_SCHEDULED,
		Attributes: &historypb.HistoryEvent_WorkflowTaskScheduledEventAttributes{WorkflowTaskScheduledEventAttributes: attributes}}
}

func episodeVideoWorkflowTaskStartedEvent(eventID int64, attributes *historypb.WorkflowTaskStartedEventAttributes) *historypb.HistoryEvent {
	return &historypb.HistoryEvent{EventId: eventID, EventType: enumspb.EVENT_TYPE_WORKFLOW_TASK_STARTED,
		Attributes: &historypb.HistoryEvent_WorkflowTaskStartedEventAttributes{WorkflowTaskStartedEventAttributes: attributes}}
}

func episodeVideoWorkflowTaskCompletedEvent(eventID int64, attributes *historypb.WorkflowTaskCompletedEventAttributes) *historypb.HistoryEvent {
	return &historypb.HistoryEvent{EventId: eventID, EventType: enumspb.EVENT_TYPE_WORKFLOW_TASK_COMPLETED,
		Attributes: &historypb.HistoryEvent_WorkflowTaskCompletedEventAttributes{WorkflowTaskCompletedEventAttributes: attributes}}
}

func episodeVideoActivityTaskScheduledEvent(eventID int64, attributes *historypb.ActivityTaskScheduledEventAttributes) *historypb.HistoryEvent {
	return &historypb.HistoryEvent{EventId: eventID, EventType: enumspb.EVENT_TYPE_ACTIVITY_TASK_SCHEDULED,
		Attributes: &historypb.HistoryEvent_ActivityTaskScheduledEventAttributes{ActivityTaskScheduledEventAttributes: attributes}}
}

func episodeVideoActivityTaskStartedEvent(eventID int64, attributes *historypb.ActivityTaskStartedEventAttributes) *historypb.HistoryEvent {
	return &historypb.HistoryEvent{EventId: eventID, EventType: enumspb.EVENT_TYPE_ACTIVITY_TASK_STARTED,
		Attributes: &historypb.HistoryEvent_ActivityTaskStartedEventAttributes{ActivityTaskStartedEventAttributes: attributes}}
}

func episodeVideoActivityTaskCompletedEvent(eventID int64, attributes *historypb.ActivityTaskCompletedEventAttributes) *historypb.HistoryEvent {
	return &historypb.HistoryEvent{EventId: eventID, EventType: enumspb.EVENT_TYPE_ACTIVITY_TASK_COMPLETED,
		Attributes: &historypb.HistoryEvent_ActivityTaskCompletedEventAttributes{ActivityTaskCompletedEventAttributes: attributes}}
}

func episodeVideoWorkflowCompletedEvent(eventID int64, attributes *historypb.WorkflowExecutionCompletedEventAttributes) *historypb.HistoryEvent {
	return &historypb.HistoryEvent{EventId: eventID, EventType: enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_COMPLETED,
		Attributes: &historypb.HistoryEvent_WorkflowExecutionCompletedEventAttributes{WorkflowExecutionCompletedEventAttributes: attributes}}
}
