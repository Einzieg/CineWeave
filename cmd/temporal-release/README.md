# Temporal Release Controller

`temporal-release` changes Worker Deployment routing independently from Worker startup. It uses the Temporal Go SDK 1.37 Worker Deployment API and never starts a Worker.

## Configuration

The following flags override their corresponding environment variables:

| Flag | Environment variable | Default |
| --- | --- | --- |
| `--address` | `CINEWEAVE_TEMPORAL_ADDRESS` | `127.0.0.1:7233` |
| `--namespace` | `CINEWEAVE_TEMPORAL_NAMESPACE` | `default` |
| `--deployment` | `CINEWEAVE_TEMPORAL_DEPLOYMENT_NAME` | required |
| `--release-id` | `CINEWEAVE_RELEASE_ID` | required |

Every command also accepts `--identity` and `--timeout`. Routing commands keep Temporal's missing-poller and missing-task-queue protections enabled unless the corresponding explicit override flag is passed.

## Commands

```powershell
$ErrorActionPreference = 'Stop'

go run ./cmd/temporal-release check
go run ./cmd/temporal-release ramp --percentage 10
go run ./cmd/temporal-release ramp --percentage 100
go run ./cmd/temporal-release promote
go run ./cmd/temporal-release drain --release-id previous-release
go run ./cmd/temporal-release rollback --release-id previous-release
```

- `check` reports current/ramping reachability, drainage, and whether the release is safe to stop.
- `ramp` assigns `(0, 100]` percent traffic. Replacing another ramp requires `--replace-ramping`.
- `promote` requires the release to be ramping at 100 percent, except for the first deployment version. `--force` performs an explicit direct cutover.
- `drain` is read-only. It waits until the release is neither current nor ramping and Temporal reports it `Drained` for consecutive observations.
- `rollback` makes the target release current and clears a different ramping release using the conflict token returned by the current-version update.

`drain` does not stop or delete Workers. Stop the old Worker release only after `safeToDecommission` is `true`.

## Docker Compose

Server deployments should use the `ops` profile so Temporal remains internal:

```powershell
$ErrorActionPreference = 'Stop'

docker compose -f compose.yml --profile ops run --rm temporal-release check --deployment cineweave-script-worker --release-id '<release-id>'
```

Append the same command flags documented above for `ramp`, `promote`, `drain`, or `rollback`. The one-shot service connects to `temporal:7233` over `cineweave_internal` and publishes no host port.
