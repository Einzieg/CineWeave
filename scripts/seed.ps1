param(
  [ValidateSet('apply', 'verify', 'validate')]
  [string]$Command = 'apply'
)

$ErrorActionPreference = 'Stop'

$utf8NoBom = [System.Text.UTF8Encoding]::new($false)
[Console]::OutputEncoding = $utf8NoBom
$OutputEncoding = $utf8NoBom

$repoRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
Push-Location $repoRoot
try {
  if ($Command -eq 'validate') {
    & go run ./cmd/cineweave-seed validate
  } else {
    & docker compose -f compose.yml --profile app run --rm seed /cineweave-seed $Command
  }
  if ($LASTEXITCODE -ne 0) {
    throw "Seed command failed with exit code $LASTEXITCODE"
  }
} finally {
  Pop-Location
}
