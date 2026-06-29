#!/usr/bin/env sh
set -eu

target="${1:-web}"

case "$target" in
  web) pnpm --filter @cineweave/web dev ;;
  api) go run ./apps/api ;;
  realtime) go run ./apps/realtime ;;
  provider-gateway) go run ./services/provider-gateway ;;
  infra) docker compose --profile infra up -d ;;
  full) docker compose --profile full up -d ;;
  *) echo "unknown target: $target" >&2; exit 1 ;;
esac

