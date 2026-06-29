# API Spec

Public APIs use JSON and a stable envelope:

```json
{
  "requestId": "req_...",
  "data": {},
  "meta": {}
}
```

Errors use:

```json
{
  "requestId": "req_...",
  "error": {
    "code": "VALIDATION_FAILED",
    "message": "request is invalid",
    "details": [],
    "retryable": false
  }
}
```

Pagination is cursor based with `limit`, `cursor`, `sort`, and `filter[field]` query parameters.

The seed OpenAPI contract is in `packages/openapi/openapi.yaml`.

Idempotent writes:

- `Idempotency-Key` header is supported by `POST /api/workflow-runs`, `POST /api/providers/models/{modelId}/test`, `POST /api/providers/manifests/test-run`, `POST /api/projects/{projectId}/assets`, and `POST /api/projects/{projectId}/assets/{assetId}/variants`.
- The same key may also be supplied as `idempotencyKey` in the JSON body for those endpoints.
- A successful replay returns `200` with the stored response and `meta.idempotentReplay=true`; a mismatched request returns `409 IDEMPOTENCY_CONFLICT`.

Workflow APIs:

- `POST /api/workflow-runs` starts a workflow. `workflowType` supports `video_production`, `text_to_storyboard`, and `script_to_storyboard`; the default is `video_production`.
- `GET /api/workflow-runs` lists runs by organization and optional project filter.
- `GET /api/workflow-runs/{workflowRunId}` returns run status and output.
- `GET /api/workflow-runs/{workflowRunId}/nodes` returns node status, retry count, input, and output for the Workflow Board.
- `GET /api/artifacts` lists generated artifacts by organization and optional project filter.

Asset APIs:

- `GET /api/projects/{projectId}/assets` lists reusable project assets, with optional `filter[type]`.
- `POST /api/projects/{projectId}/assets` creates a managed asset and supports `Idempotency-Key`.
- `POST /api/projects/{projectId}/assets/upload-url` returns a presigned S3-compatible PUT URL and deterministic `storageKey`; it does not create a DB row by itself.
- `POST /api/projects/{projectId}/assets/{assetId}/variants` records an uploaded media variant by writing `artifacts`, `media_files`, and `media_variants`, then updates `assets.current_artifact_id`.

Provider test APIs:

- Provider model tests and declarative manifest test-runs persist the idempotency key to `provider_call_logs.idempotency_key` when supplied.

Prompt APIs:

- `GET /api/prompt-templates` lists system and organization prompt templates with their active version summary.
- `POST /api/prompt-templates/{templateId}/versions` creates a new immutable prompt version; `POST /api/prompt-versions/{versionId}/activate` makes it the active version.
- `GET /api/prompt-bindings` and `POST /api/prompt-bindings` manage organization/project prompt overrides.
- `POST /api/prompts/render-test` resolves project > organization > system prompt priority and renders safe dot-path variables without executing code.

Provider webhook API:

- `POST /api/provider-webhooks/{providerAccountId}/{webhookSecret}` receives async provider callbacks.
- The webhook secret is read from the provider account config as `webhookSecret`.
- The payload must include `externalTaskId`, `taskId`, or `id`.
- The handler updates `provider_async_tasks`, updates the related `provider_call_logs` row, writes `provider.webhook.received` to `event_outbox`, and signals the linked Temporal workflow when the provider call has `workflow_run_id`.
