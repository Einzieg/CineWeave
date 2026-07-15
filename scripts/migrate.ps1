param(
  [ValidateSet('up', 'status', 'version', 'down', 'down-to', 'reset', 'validate')]
  [string]$Command = 'up',
  [long]$Target = 0
)

$ErrorActionPreference = 'Stop'

$utf8NoBom = [System.Text.UTF8Encoding]::new($false)
[Console]::OutputEncoding = $utf8NoBom
$OutputEncoding = $utf8NoBom

$repoRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
Push-Location $repoRoot
try {
  if ($Command -eq 'validate') {
    & go run ./cmd/cineweave-migrate validate
  } else {
    $toolArgs = @('/cineweave-migrate', $Command)
    if ($Command -eq 'down-to') {
      $toolArgs += [string]$Target
    }
    & docker compose -f compose.yml --profile app run --rm migrate @toolArgs
  }
  if ($LASTEXITCODE -ne 0) {
    throw "Migration command failed with exit code $LASTEXITCODE"
  }
} finally {
  Pop-Location
}
