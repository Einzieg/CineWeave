# CineWeave

CineWeave is a cloud-native AI video production platform rebuilt around Provider Gateway, Temporal workflows, multi-tenant access control, Artifact storage, and observable provider execution.

The repository root is this directory. Do not create a nested `cineweave/` folder.

## Quick Start

```powershell
pnpm install
docker compose -f compose.yml --profile app up -d --build
```

The web container is exposed at `http://localhost:3000`. On a fresh database, open that URL and complete `/setup` to create the first administrator, organization, and workspace. Public registration is disabled by default with `CINEWEAVE_ALLOW_PUBLIC_REGISTRATION=false`; keep it disabled for server deployments unless you intentionally want open signup.

After setup, use `/login` with the administrator account. The web app stores the login session locally and sends the access token plus organization context automatically; users do not manually enter tokens, organization IDs, or workspace IDs.

Useful commands:

```powershell
go test ./...
pnpm --filter @cineweave/web typecheck
pnpm --filter @cineweave/web lint
docker compose config
docker compose -f compose.yml build api provider-gateway script-worker media-worker web
```

The current MVP is silent video. TTS, generated audio artifacts, audio mix, subtitles, and BGM are intentionally deferred.

## Provider Gateway Boundary

Provider Gateway is required by default for upstream model access. API and worker services should call `PROVIDER_GATEWAY_URL` with `CINEWEAVE_SERVICE_TOKEN`; production must not enable direct provider fallback. `CINEWEAVE_ALLOW_PROVIDER_DIRECT_FALLBACK=true` is only for local development or test troubleshooting.

Provider Gateway now owns `text.generate`, `text.stream`, and `image.generate` runtime calls. The image runtime targets OpenAI-compatible `/v1/images/generations`, accepts URL or `b64_json` upstream responses, downloads or decodes the media inside the Gateway, stores it in S3 / MinIO, and writes `media_files`, `artifacts`, `provider_call_logs`, and `cost_records`. Private or localhost upstream media URLs are blocked unless `CINEWEAVE_ALLOW_PRIVATE_PROVIDER_MEDIA_URLS=true` is explicitly set for development.

Provider Gateway Video Runtime v1 adds declarative HTTP video providers through `/internal/provider/video/create-task`, `/internal/provider/video/poll-task`, and `/internal/provider/video/cancel-task`. Video providers should be onboarded with Provider Manifest endpoints first because upstream video APIs vary widely. `provider_async_tasks` is the durable async task state source; Temporal workers will own later durable polling loops, while the Gateway performs each create / poll / cancel call, downloads completed video media, writes S3 / MinIO objects, and records `media_files`, `artifacts`, `provider_call_logs`, and final `cost_records`. Video downloads default to `CINEWEAVE_PROVIDER_VIDEO_MAX_BYTES=536870912`. Private or localhost upstream media URLs remain blocked by default; only set `CINEWEAVE_ALLOW_PRIVATE_PROVIDER_MEDIA_URLS=true` for controlled development providers that require it.

Provider limits are enforced only inside Provider Gateway. `provider_limit_policies` can cap max concurrency, requests per minute/day, daily/monthly budget, and failure circuit behavior by organization, account, model, and task type. `provider_leases` protects active upstream calls, budget checks read `cost_records`, and circuit state is stored in `provider_circuit_states`. Guard-blocked calls are written to `provider_call_logs` with `status=blocked` and do not create `cost_records`.

Model profiles can bind multiple provider models. Provider Gateway supports `priority`, `priority_with_fallback`, `weighted`, `cost_optimized`, and `latency_optimized` routing strategies, and every Gateway text/image/video-create response can include an `attempts` summary. Guard-blocked and retryable upstream failures can fall back to the next candidate according to `fallback_strategy`; `AUTH_FAILED`, `MODEL_NOT_FOUND`, `INVALID_REQUEST`, `UNSUPPORTED_CAPABILITY`, and `CONTENT_REJECTED` stop by default. Text streaming only falls back before the first delta is sent. Video poll/cancel are pinned to the `provider_async_tasks` row created by video create-task and are not rerouted.

## Prompt Registry

System prompts are seeded during migration for `storyboard_planner`, `storyboard_image_prompt`, and `storyboard_video_prompt`. Prompt templates are versioned through `prompt_versions`; active versions are immutable operational records, and prompt edits should create a new version before activation.

Workflow prompt resolution follows project binding, organization binding, organization active version, then system active version. `text_to_storyboard` and `video_production` render prompt templates through the Prompt Registry before calling Provider Gateway. Each new workflow model call sends `promptVersionId`, `promptHash`, `promptTemplateKey`, and `promptSource`; Provider Gateway writes `provider_call_logs.prompt_version_id`, `provider_call_logs.prompt_hash`, and gateway-side Artifact metadata for image/video outputs. Storyboard JSON artifacts also store `artifacts.prompt_hash` plus `metadata.promptVersionId`, `metadata.promptTemplateKey`, and `metadata.promptSource`.

Prompt management APIs are available at `/api/prompt-templates`, `/api/prompt-templates/{templateId}/versions`, `/api/prompt-versions/{versionId}/activate`, `/api/prompt-bindings`, and `/api/prompts/render-test`. `prompt.read` allows listing and render-test; `prompt.manage` allows creating templates, versions, bindings, and activating versions.

`text_to_storyboard` is the first real storyboard workflow path. `POST /api/workflow-runs` with `workflowType=text_to_storyboard` starts Temporal, the script worker calls Provider Gateway for `text.generate` using `script_agent_default`, then records the storyboard JSON artifact and normalized `storyboard_shots`. It does not call image or video Gateway paths.

`video_production` v1 now generates up to 3 storyboard shots sequentially. Each shot gets its own Provider Gateway `image.generate` output and async `video.create_task` / `video.poll_task` `generated_video` artifact, with links persisted on `storyboard_shots`. After all shot videos succeed, the Media Worker runs FFmpeg to normalize and concatenate the clips into a `final_video` MP4 artifact and writes a `timeline_json` manifest artifact. `POST /api/workflow-runs` accepts optional `input.duration`, `input.aspectRatio`, `input.resolution`, `input.pollIntervalSeconds`, `input.maxPolls`, `input.maxShots` (capped at 3), and `input.skipCompose` for debugging.

`media-worker` listens on Temporal task queue `cineweave-media`. It registers final-video composition and project export activities, using `media_files`, `artifacts`, `project_exports`, and S3 / MinIO object storage; it does not call Provider Gateway or access provider credentials. The Docker Compose media-worker image uses `deploy/docker-compose/Dockerfile-media-worker` and installs FFmpeg in that runtime image.

Shot results are available through `GET /api/workflow-runs/{id}/shots?includePreviewUrl=true`, and the project workspace shows storyboard shots with image/video previews while retaining the Vault artifact list.

Video workflow cancellation is exposed through `POST /api/workflow-runs/{id}/cancel`. Running, queued, or already-cancelling runs are marked `cancelling` and API requests Temporal cancellation; terminal runs return their current state for repeated cancel calls. If the current shot has a running Provider Gateway video async task, workflow cleanup calls `/internal/provider/video/cancel-task`; completed shots stay succeeded and not-yet-started shots are marked cancelled.

The Vault preview path uses authenticated API endpoints to create short-lived signed GET URLs for `artifacts` and `media_files`; S3 / MinIO buckets do not need public read access. In local Docker Compose, server components use `S3_ENDPOINT=http://minio:9000`, while browser preview URLs are signed with `S3_PUBLIC_ENDPOINT=http://localhost:9000`.

For Docker Compose deployments, configure provider accounts and bind active models to `script_agent_default`, `image_generation_default`, and `video_generation_default` before running `video_production`. Missing bindings fail the workflow with `MODEL_PROFILE_NOT_CONFIGURED`.

## RBAC Authorization

API access is permission based through `role_bindings` and `role_permissions`, not raw membership checks. Register creates an organization, active membership, and an `org_owner` binding for the creator. Project creation grants the creator `project_owner`. Provider, prompt, workflow, asset, artifact, media, team, and role-binding operations are checked with fine-grained permissions such as `provider.manage`, `prompt.read`, `prompt.manage`, `workflow.run`, `workflow.cancel`, `asset.write`, `artifact.read`, and `role.manage`. Organization bindings inherit to workspaces and projects; workspace bindings inherit to projects in that workspace; project bindings apply only to that project. Team role bindings apply only to active team members.

## Layout

- `apps/api`: Go public API server.
- `apps/realtime`: Go realtime gateway.
- `apps/web`: Next.js AI video creation workbench.
- `services/provider-gateway`: CineWeave Gateway service.
- `workers`: Temporal worker entry points.
- `internal`: shared Go packages.
- `packages`: OpenAPI, provider manifest schema, generated/shared types.
- `db`: migrations and seeds.
- `deploy`: Docker Compose, Kubernetes, Helm, and ingress assets.
- `docs`: implementation-facing architecture and execution notes.
