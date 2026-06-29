# CineWeave

CineWeave is a cloud-native AI video production platform rebuilt around Provider Gateway, Temporal workflows, multi-tenant access control, Artifact storage, and observable provider execution.

The repository root is this directory. Do not create a nested `cineweave/` folder.

## Quick Start

```powershell
pnpm install
docker compose up -d
pnpm --filter @cineweave/web dev
```

Useful commands:

```powershell
go test ./...
pnpm --filter @cineweave/web typecheck
pnpm --filter @cineweave/web lint
docker compose config
```

## Provider Gateway Boundary

Provider Gateway is required by default for upstream model access. API and worker services should call `PROVIDER_GATEWAY_URL` with `CINEWEAVE_SERVICE_TOKEN`; production must not enable direct provider fallback. `CINEWEAVE_ALLOW_PROVIDER_DIRECT_FALLBACK=true` is only for local development or test troubleshooting.

Provider Gateway now owns `text.generate`, `text.stream`, and `image.generate` runtime calls. The image runtime targets OpenAI-compatible `/v1/images/generations`, accepts URL or `b64_json` upstream responses, downloads or decodes the media inside the Gateway, stores it in S3 / MinIO, and writes `media_files`, `artifacts`, `provider_call_logs`, and `cost_records`. Private or localhost upstream media URLs are blocked unless `CINEWEAVE_ALLOW_PRIVATE_PROVIDER_MEDIA_URLS=true` is explicitly set for development.

Provider Gateway Video Runtime v1 adds declarative HTTP video providers through `/internal/provider/video/create-task`, `/internal/provider/video/poll-task`, and `/internal/provider/video/cancel-task`. Video providers should be onboarded with Provider Manifest endpoints first because upstream video APIs vary widely. `provider_async_tasks` is the durable async task state source; Temporal workers will own later durable polling loops, while the Gateway performs each create / poll / cancel call, downloads completed video media, writes S3 / MinIO objects, and records `media_files`, `artifacts`, `provider_call_logs`, and final `cost_records`. Video downloads default to `CINEWEAVE_PROVIDER_VIDEO_MAX_BYTES=536870912`.

`text_to_storyboard` is the first real storyboard workflow path. `POST /api/workflow-runs` with `workflowType=text_to_storyboard` starts Temporal, the script worker calls Provider Gateway for `text.generate` using `script_agent_default`, then calls Provider Gateway for `image.generate` using `image_generation_default`. The worker records workflow node state and the storyboard JSON artifact only; Provider Gateway owns upstream credentials, image media storage, provider call logs, and cost records.

`video_production` v1 now runs the minimum real production chain: text to `storyboard_json`, image to `generated_image`, and async image-to-video to `generated_video`. `POST /api/workflow-runs` accepts optional `input.duration`, `input.aspectRatio`, `input.resolution`, `input.pollIntervalSeconds`, and `input.maxPolls`; Temporal creates the video task once through Provider Gateway and uses a durable workflow-level poll loop until the Gateway returns a terminal video result.

For Docker Compose deployments, configure provider accounts and bind active models to `script_agent_default`, `image_generation_default`, and `video_generation_default` before running `video_production`. Missing bindings fail the workflow with `MODEL_PROFILE_NOT_CONFIGURED`.

## Layout

- `apps/api`: Go public API server.
- `apps/realtime`: Go realtime gateway.
- `apps/web`: Next.js Studio web app.
- `services/provider-gateway`: CineWeave Gateway service.
- `workers`: Temporal worker entry points.
- `internal`: shared Go packages.
- `packages`: OpenAPI, provider manifest schema, generated/shared types.
- `db`: migrations and seeds.
- `deploy`: Docker Compose, Kubernetes, Helm, and ingress assets.
- `docs`: implementation-facing architecture and execution notes.
