# Workflow Engine

CineWeave uses Temporal for long-running production pipelines.

Initial workflow targets:

- TextToStoryboardWorkflow
- ScriptToStoryboardWorkflow
- StoryboardToImageWorkflow
- StoryboardToVideoWorkflow
- VideoComposeWorkflow
- VideoProductionWorkflow

Early Phase 7 MVP flow:

1. `ScriptToStoryboardWorkflow` creates a `storyboard` artifact.
2. `StoryboardToImageWorkflow` creates an `image_collection` artifact.
3. `StoryboardToVideoWorkflow` creates a `video_clips` artifact.
4. `VideoComposeWorkflow` creates a `final_video` artifact.
5. `QualityCheck` creates a `quality_report` artifact and completes the workflow run.

Current `VideoProductionWorkflow` executes the real Provider Gateway path: storyboard text, per-shot image generation, per-shot async video create/poll, then Media Worker composition. `ComposeFinalVideo` runs on Temporal task queue `cineweave-media`, uses FFmpeg, writes `timeline_json` and `final_video` artifacts, and does not call Provider Gateway.

Each node writes a `workflow_node_runs` row, stores generated artifacts in S3 / MinIO, emits `event_outbox` entries, and uses Temporal activity retries. The node `retry_count` column reflects retry attempts after a node has already been started.

Every Activity must be idempotent, have timeout and retry policy, and record related provider calls, artifacts, and cost where applicable.
