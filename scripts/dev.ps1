param(
  [ValidateSet("web", "api", "realtime", "provider-gateway", "infra", "full")]
  [string]$Target = "web"
)

switch ($Target) {
  "web" { pnpm --filter @cineweave/web dev }
  "api" { go run ./apps/api }
  "realtime" { go run ./apps/realtime }
  "provider-gateway" { go run ./services/provider-gateway }
  "infra" { docker compose --profile infra up -d }
  "full" { docker compose --profile full up -d }
}

