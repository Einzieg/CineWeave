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
