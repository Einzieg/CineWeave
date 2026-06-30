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

Host ports are configurable through environment variables such as `CINEWEAVE_API_HOST_PORT`, `CINEWEAVE_REALTIME_HOST_PORT`, and `CINEWEAVE_WEB_HOST_PORT`.
