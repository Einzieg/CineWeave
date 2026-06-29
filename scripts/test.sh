#!/usr/bin/env sh
set -eu

go test ./...
pnpm --filter @cineweave/web typecheck
pnpm --filter @cineweave/web lint

