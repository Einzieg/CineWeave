param(
  [switch]$SkipMigrationIntegration,
  [switch]$SkipImageBuild,
  [switch]$CheckProviderDrain,
  [switch]$RequireClean
)

$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true

$utf8NoBom = [System.Text.UTF8Encoding]::new($false)
[Console]::OutputEncoding = $utf8NoBom
$OutputEncoding = $utf8NoBom

$repoRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path

function Invoke-ReleaseStep {
  param(
    [Parameter(Mandatory = $true)]
    [string]$Name,
    [Parameter(Mandatory = $true)]
    [scriptblock]$Action
  )
  Write-Host "`n==> $Name"
  & $Action
}

function Assert-GoVersion {
  $raw = (& go env GOVERSION).Trim()
  if ($raw -notmatch '^go(?<version>\d+\.\d+\.\d+)$') {
    throw "Cannot parse Go version: $raw"
  }
  $actual = [Version]$Matches.version
  $minimum = [Version]'1.26.5'
  if ($actual -lt $minimum) {
    throw "Go $minimum or newer is required; found $actual"
  }
  Write-Host "Go toolchain: $actual"
}

function Assert-NoTrackedSecrets {
  $patterns = @(
    'sk-[A-Za-z0-9_-]{20,}',
    'sk-or-v1-[A-Za-z0-9]{20,}',
    'AKIA[0-9A-Z]{16}',
    '-----BEGIN (RSA |EC |OPENSSH )?PRIVATE KEY-----'
  )
  $previousNativePreference = $PSNativeCommandUseErrorActionPreference
  $PSNativeCommandUseErrorActionPreference = $false
  try {
    foreach ($pattern in $patterns) {
      $matches = & git grep -n -I -E -- $pattern -- ':!pnpm-lock.yaml' ':!go.sum' 2>$null
      $exitCode = $LASTEXITCODE
      if ($exitCode -eq 0) {
        throw "Tracked secret-like value matched ${pattern}:`n$($matches -join "`n")"
      }
      if ($exitCode -ne 1) {
        throw "git grep failed while scanning tracked secrets (exit $exitCode)"
      }
    }
    $trackedLocalEnvironment = @(& git ls-files -- .env .env.override)
    if ($LASTEXITCODE -ne 0) {
      throw "git ls-files failed while checking local environment files"
    }
    if ($trackedLocalEnvironment.Count -gt 0) {
      throw "Local environment files are tracked: $($trackedLocalEnvironment -join ', ')"
    }
  } finally {
    $PSNativeCommandUseErrorActionPreference = $previousNativePreference
  }
  Write-Host 'Tracked secret scan passed.'
}

Push-Location $repoRoot
try {
  if ($RequireClean -and (git status --porcelain)) {
    throw 'Release worktree is not clean.'
  }

  Invoke-ReleaseStep 'Toolchain version' { Assert-GoVersion }
  Invoke-ReleaseStep 'Tracked secret scan' { Assert-NoTrackedSecrets }
  Invoke-ReleaseStep 'Go vet' { go vet ./... }
  Invoke-ReleaseStep 'Go vulnerability scan' { go run golang.org/x/vuln/cmd/govulncheck@latest ./... }
  Invoke-ReleaseStep 'Node dependency audit' { pnpm audit --audit-level moderate }
  Invoke-ReleaseStep 'Repository validation' { pnpm run test }
  Invoke-ReleaseStep 'Web production build' { pnpm --filter @cineweave/web build }
  Invoke-ReleaseStep 'Compose validation' { docker compose -f compose.yml config --quiet }

  if (-not $SkipMigrationIntegration) {
    Invoke-ReleaseStep 'Isolated migration roundtrip and baseline equivalence' {
      pwsh -NoProfile -File scripts/test-migrations.ps1
    }
  }
  if ($CheckProviderDrain) {
    Invoke-ReleaseStep 'Provider runtime drain check' {
      pwsh -NoProfile -File scripts/provider-data-guard.ps1 -Mode DrainCheck
    }
  }
  if (-not $SkipImageBuild) {
    Invoke-ReleaseStep 'Application image build' {
      docker compose -f compose.yml --profile app build
    }
  }

  Write-Host "`nRelease checks passed."
} finally {
  Pop-Location
}
