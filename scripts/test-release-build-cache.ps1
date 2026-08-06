[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true
Set-StrictMode -Version Latest

$repoRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
$goDockerfiles = @(
  'deploy/docker-compose/Dockerfile-go',
  'deploy/docker-compose/Dockerfile-provider-gateway',
  'deploy/docker-compose/Dockerfile-media-worker',
  'deploy/docker-compose/Dockerfile-database-tools',
  'deploy/docker-compose/Dockerfile-temporal-schema'
)

foreach ($relativePath in $goDockerfiles) {
  $source = Get-Content -LiteralPath (Join-Path $repoRoot $relativePath) -Raw -Encoding UTF8
  foreach ($required in @(
    '# syntax=docker/dockerfile:1.7@sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e',
    'RUN --mount=type=cache,target=/go/pkg/mod go mod download',
    '--mount=type=cache,target=/root/.cache/go-build'
  )) {
    if (-not $source.Contains($required, [StringComparison]::Ordinal)) {
      throw "$relativePath is missing the release build cache contract: $required"
    }
  }
}

$webDockerfile = 'apps/web/Dockerfile'
$webSource = Get-Content -LiteralPath (Join-Path $repoRoot $webDockerfile) -Raw -Encoding UTF8
foreach ($required in @(
  '# syntax=docker/dockerfile:1.7@sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e',
  'RUN --mount=type=cache,target=/root/.local/share/pnpm/store'
)) {
  if (-not $webSource.Contains($required, [StringComparison]::Ordinal)) {
    throw "$webDockerfile is missing the release build cache contract: $required"
  }
}

Write-Host 'Release BuildKit cache contract passed.'
