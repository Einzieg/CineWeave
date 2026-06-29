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

