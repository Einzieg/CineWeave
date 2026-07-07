# Docker Compose

The root `compose.yml` is the entry point for local development.

```powershell
docker compose up -d
docker compose --profile app up -d
docker compose --profile full up -d
```

Infrastructure services start without requiring an explicit profile. Application services are available through the `app` and `full` profiles.

For server-style deployment, prefer Compose-managed application processes instead of local binaries:

```powershell
docker compose --profile app up -d --build migrate api script-worker media-worker realtime event-publisher provider-gateway web
```

The `migrate` service applies all `db/migrations/*.up.sql` files and application services wait for it before starting.

`media-worker` runs FFmpeg-based final video composition on Temporal task queue `cineweave-media`. Its image uses `deploy/docker-compose/Dockerfile-media-worker`, which installs FFmpeg only for the media worker runtime.

Set a stable `CINEWEAVE_CREDENTIAL_MASTER_KEY` in the deployment environment before creating provider credentials. Rotating or losing this value makes existing encrypted provider credentials unreadable.

Only browser-facing services are mapped to the host by default:

- `CINEWEAVE_WEB_HOST_PORT` defaults to `19285`.
- `CINEWEAVE_API_HOST_PORT` defaults to `19288`.
- `CINEWEAVE_REALTIME_HOST_PORT` defaults to `19281`.
- `MINIO_API_HOST_PORT` defaults to `19290` so local signed preview URLs can load in the browser.

PostgreSQL, Redis, NATS, Temporal, Provider Gateway, and the MinIO console are intentionally reachable only on the Docker network. If you need host access for debugging, add a local Compose override instead of changing the default server deployment file.
