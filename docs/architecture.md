# CineWeave Architecture

CineWeave is a cloud-native AI video production platform organized around five core planes:

- Control Plane: Go API server for users, organizations, workspaces, projects, RBAC, assets, workflows, and provider management.
- Realtime Plane: Go realtime gateway for SSE first workflow and queue events.
- Workflow Plane: Temporal for durable workflow execution, retries, cancellation, and history.
- Provider Plane: CineWeave Gateway as the only upstream AI model access path.
- Data Plane: PostgreSQL, Redis, S3-compatible object storage, and NATS JetStream.

The repository root is `D:\Code\CineWeave`. The `cineweave/` wrapper shown in the technical specification is a logical repository label, not a nested directory to create.

## Boundary Rules

- Workers, workflow activities, and public API handlers must not call upstream model APIs directly.
- Provider Gateway owns provider calls, credential decryption, response normalization, media download, S3 / MinIO transfer, provider call logs, cost records, and gateway-side Artifact / media file records.
- Temporal owns durable workflow execution state. PostgreSQL owns business read models and audit state.
- NATS JetStream distributes events and is not a task queue replacement for Temporal.
- `services/event-publisher` is the reliable bridge from PostgreSQL `event_outbox` to NATS JetStream. It claims pending outbox rows, publishes them to `cineweave.events.>`, and marks rows as `published` after JetStream acknowledgement.
- Redis is for cache, short-lived state, and rate-limit counters only.
