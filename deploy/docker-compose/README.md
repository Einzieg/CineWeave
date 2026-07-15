# Docker Compose

The root `compose.yml` is the entry point for local development.

```powershell
docker compose -f compose.yml up -d
docker compose -f compose.yml --profile app up -d
docker compose -f compose.yml --profile full up -d
```

Infrastructure services start without requiring an explicit profile. Application services are available through the `app` and `full` profiles.

For server-style deployment, prefer Compose-managed application processes instead of local binaries:

```powershell
docker compose -f compose.yml --profile app up -d --build
```

Compose starts database initialization in this order:

```text
postgres -> migrate -> seed -> application services
```

`migrate` runs the embedded Goose migrations through `cineweave-migrate`. `seed` then loads versioned system resources for RBAC, Provider Catalog, model capabilities, Prompt Registry, and project manuals. Application services wait for both one-shot services to exit successfully.

The Goose ledger and migration audit live in the `cineweave_migrations` schema. Production deployments must set an immutable `CINEWEAVE_RELEASE_ID`; destructive `down`, `down-to`, and `reset` commands are rejected when `CINEWEAVE_ENV=production`.

## Temporal Worker releases

Temporal schema updates run through the explicit `temporal-schema` one-shot service before the `1.31.2` Server starts. The `temporal-namespace` one-shot service creates the configured namespace idempotently. The Server container does not run `auto-setup`.

Production Workers are deployed separately with `worker-release.compose.yml`. Set an immutable release ID, immutable image references, a unique project name, and the shared internal network before starting the green release:

```powershell
$ErrorActionPreference = 'Stop'
$env:CINEWEAVE_RELEASE_ID = '20260714-a1b2c3d4.17'
$env:CINEWEAVE_WORKER_PROJECT_NAME = 'cineweave-workers-20260714-a1b2c3d4-17'
$env:CINEWEAVE_SCRIPT_WORKER_IMAGE = 'registry.example/cineweave/script-worker@sha256:...'
$env:CINEWEAVE_AGENT_WORKER_IMAGE = 'registry.example/cineweave/agent-worker@sha256:...'
$env:CINEWEAVE_MEDIA_WORKER_IMAGE = 'registry.example/cineweave/media-worker@sha256:...'
$env:CINEWEAVE_AUDIO_WORKER_IMAGE = 'registry.example/cineweave/audio-worker@sha256:...'
docker compose -f deploy/docker-compose/worker-release.compose.yml up -d
```

Worker startup only registers Deployment Versions. On a Compose deployment, run `docker compose -f compose.yml --profile ops run --rm temporal-release ...` for `check`, `ramp`, `promote`, `drain`, and `rollback`; see `cmd/temporal-release/README.md`. The one-shot controller stays on the Docker network and does not require a host Temporal port. Do not stop the previous Worker project until `drain` reports that every old Pinned version is safe to decommission.

Local administration uses the same binaries as Compose:

```powershell
pwsh -File scripts/migrate.ps1 -Command status
pwsh -File scripts/seed.ps1 -Command verify
```

`media-worker` runs FFmpeg-based final video composition on Temporal task queue `cineweave-media`. Its image uses `deploy/docker-compose/Dockerfile-media-worker`, which installs FFmpeg only for the media worker runtime.

Set a stable `CINEWEAVE_CREDENTIAL_MASTER_KEY` in the deployment environment before creating provider credentials. Rotating or losing this value makes existing encrypted provider credentials unreadable.

Only browser-facing services are mapped to the host by default:

- `CINEWEAVE_WEB_HOST_PORT` defaults to `19285`.
- `CINEWEAVE_API_HOST_PORT` defaults to `19288`.
- `CINEWEAVE_REALTIME_HOST_PORT` defaults to `19281`.
- `MINIO_API_HOST_PORT` defaults to `19290` so local signed preview URLs can load in the browser.

PostgreSQL, Redis, NATS, Temporal, Provider Gateway, and the MinIO console are intentionally reachable only on the Docker network. If you need host access for debugging, add a local Compose override instead of changing the default server deployment file.
