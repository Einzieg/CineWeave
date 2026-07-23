param(
  [switch]$SkipBuild
)

$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true

$utf8NoBom = [System.Text.UTF8Encoding]::new($false)
[Console]::OutputEncoding = $utf8NoBom
$OutputEncoding = $utf8NoBom

$repoRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
Push-Location $repoRoot
try {
  if (-not $SkipBuild) {
    pnpm --filter @cineweave/web build
  }

  $webRoot = Join-Path $repoRoot 'apps/web'
  $standaloneWebRoot = Join-Path $webRoot '.next/standalone/apps/web'
  $standaloneServer = Join-Path $standaloneWebRoot 'server.js'
  if (-not (Test-Path -LiteralPath $standaloneServer)) {
    throw "Standalone Web server was not built: $standaloneServer"
  }

  $staticSource = Join-Path $webRoot '.next/static'
  $staticTarget = Join-Path $standaloneWebRoot '.next/static'
  New-Item -ItemType Directory -Path $staticTarget -Force | Out-Null
  Get-ChildItem -LiteralPath $staticSource -Force | Copy-Item -Destination $staticTarget -Recurse -Force

  $publicSource = Join-Path $webRoot 'public'
  $publicTarget = Join-Path $standaloneWebRoot 'public'
  New-Item -ItemType Directory -Path $publicTarget -Force | Out-Null
  Get-ChildItem -LiteralPath $publicSource -Force | Copy-Item -Destination $publicTarget -Recurse -Force

  pnpm --filter @cineweave/web test:e2e:commerce
} finally {
  Pop-Location
}
