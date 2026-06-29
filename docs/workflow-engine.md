# Workflow Engine

CineWeave uses Temporal for long-running production pipelines.

Initial workflow targets:

- TextToStoryboardWorkflow
- ScriptToStoryboardWorkflow
- StoryboardToImageWorkflow
- StoryboardToVideoWorkflow
- VideoComposeWorkflow
- VideoProductionWorkflow

Implemented Phase 7 MVP flow:

1. `ScriptToStoryboardWorkflow` creates a `storyboard` artifact.
2. `StoryboardToImageWorkflow` creates an `image_collection` artifact.
3. `StoryboardToVideoWorkflow` creates a `video_clips` artifact.
4. `VideoComposeWorkflow` creates a `final_video` artifact.
5. `QualityCheck` creates a `quality_report` artifact and completes the workflow run.

`VideoProductionWorkflow` executes the five nodes above in order. Each node writes a `workflow_node_runs` row, stores a JSON artifact in MinIO, emits `event_outbox` entries, and uses Temporal activity retries. The node `retry_count` column reflects retry attempts after a node has already been started.

Every Activity must be idempotent, have timeout and retry policy, and record related provider calls, artifacts, and cost where applicable.
