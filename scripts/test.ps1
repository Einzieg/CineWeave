$ErrorActionPreference = "Stop"

go test ./...
pnpm --filter @cineweave/web typecheck
pnpm --filter @cineweave/web lint

