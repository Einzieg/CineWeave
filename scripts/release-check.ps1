param(
  [switch]$SkipMigrationIntegration,
  [switch]$SkipCommerceBrowserE2E,
  [switch]$SkipImageBuild,
  [switch]$RunCommerceRealProviderSmoke,
  [switch]$CheckProviderDrain,
  [switch]$RequireClean,
  [string]$ReleaseId = '',
  [string]$EvidencePath = 'tmp/release-prepare-evidence.json',
  [string]$ComposeFile = 'compose.yml'
)

$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true
Set-StrictMode -Version Latest

$utf8NoBom = [System.Text.UTF8Encoding]::new($false)
[Console]::OutputEncoding = $utf8NoBom
$OutputEncoding = $utf8NoBom

$repoRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
$evidenceModule = Join-Path $repoRoot 'scripts/release-evidence.psm1'
Import-Module $evidenceModule -Force
$resolvedComposeFile = if ([IO.Path]::IsPathRooted($ComposeFile)) {
  (Resolve-Path -LiteralPath $ComposeFile).Path
} else {
  (Resolve-Path -LiteralPath (Join-Path $repoRoot $ComposeFile)).Path
}

function Invoke-ReleaseStep {
  param(
    [Parameter(Mandatory = $true)]
    [string]$Name,
    [Parameter(Mandatory = $true)]
    [scriptblock]$Action
  )
  Write-Host "`n==> $Name"
  Invoke-ReleaseEvidenceStep `
    -State $releaseEvidence `
    -EvidencePath $resolvedEvidencePath `
    -Name $Name `
    -Action $Action
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

function Get-PreparedImageIdentities {
  $imageNames = @(
    & docker compose -f $resolvedComposeFile --profile app config --images |
      ForEach-Object { ([string]$_).Trim() } |
      Where-Object { $_ -like 'cineweave-*' } |
      Sort-Object -Unique
  )
  if ($LASTEXITCODE -ne 0) {
    throw 'Unable to resolve prepared Compose image names.'
  }
  if ($imageNames.Count -eq 0) {
    throw 'No prepared CineWeave images were found in the app profile.'
  }

  $identities = foreach ($imageName in $imageNames) {
    $imageId = (
      & docker image inspect --format '{{.Id}}' $imageName |
        ForEach-Object { [string]$_ }
    ) -join ''
    if ($LASTEXITCODE -ne 0) {
      throw "Unable to inspect prepared image: $imageName"
    }
    $imageId = $imageId.Trim()
    if ($imageId -notmatch '^sha256:[0-9a-f]{64}$') {
      throw "Prepared image $imageName has an invalid image ID: $imageId"
    }
    [pscustomobject]@{
      name = $imageName
      imageId = $imageId
    }
  }
  return @($identities)
}

$releaseFailure = $null
$releaseEvidence = $null
$resolvedEvidencePath = $null
Push-Location $repoRoot
try {
  $coreCommit = (& git rev-parse HEAD).Trim()
  if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($coreCommit)) {
    throw 'Unable to resolve the Core release commit.'
  }
  $repositoryStatus = @(git status --porcelain)
  if ($LASTEXITCODE -ne 0) {
    throw 'Unable to read the Core worktree status.'
  }
  $repositoryClean = $repositoryStatus.Count -eq 0
  if ($RequireClean -and -not $repositoryClean) {
    throw 'Release worktree is not clean.'
  }
  if ([string]::IsNullOrWhiteSpace($ReleaseId)) {
    $ReleaseId = $coreCommit
  }
  $resolvedEvidencePath = if ([IO.Path]::IsPathRooted($EvidencePath)) {
    [IO.Path]::GetFullPath($EvidencePath)
  } else {
    [IO.Path]::GetFullPath((Join-Path $repoRoot $EvidencePath))
  }
  $releaseEvidence = New-ReleaseEvidenceState `
    -Phase 'prepare' `
    -ReleaseId $ReleaseId `
    -CoreCommit $coreCommit `
    -RepositoryClean $repositoryClean `
    -Options @{
      migrationIntegration = -not $SkipMigrationIntegration
      commerceBrowserE2E = -not $SkipCommerceBrowserE2E
      imageBuild = -not $SkipImageBuild
      providerDrain = [bool]$CheckProviderDrain
      paidProviderSmoke = [bool]$RunCommerceRealProviderSmoke
    }
  Write-ReleaseEvidence -State $releaseEvidence -Path $resolvedEvidencePath

  Invoke-ReleaseStep 'Toolchain version' { Assert-GoVersion }
  Invoke-ReleaseStep 'Tracked secret scan' { Assert-NoTrackedSecrets }
  Invoke-ReleaseStep 'Go vet' { go vet ./... }
  Invoke-ReleaseStep 'Go vulnerability scan' { go run golang.org/x/vuln/cmd/govulncheck@latest ./... }
  Invoke-ReleaseStep 'Node dependency audit' { pnpm audit --audit-level moderate }
  Invoke-ReleaseStep 'Repository validation' { pnpm run test }
  Invoke-ReleaseStep 'Web production build' { pnpm --filter @cineweave/web build }
  Invoke-ReleaseStep 'Compose validation' { docker compose -f $resolvedComposeFile config --quiet }

  if (-not $SkipCommerceBrowserE2E) {
    Invoke-ReleaseStep 'Commerce browser E2E' {
      pwsh -NoProfile -File scripts/test-commerce-e2e.ps1 -SkipBuild
    }
  }

  if (-not $SkipMigrationIntegration) {
    Invoke-ReleaseStep 'Isolated migration roundtrip and baseline equivalence' {
      pwsh -NoProfile -File scripts/test-migrations.ps1
    }
  }
  if ($CheckProviderDrain) {
    Invoke-ReleaseStep 'Provider runtime drain check' {
      pwsh -NoProfile -File scripts/provider-data-guard.ps1 -Mode DrainCheck -ComposeFile $resolvedComposeFile
    }
  }
  if ($RunCommerceRealProviderSmoke) {
    Invoke-ReleaseStep 'Commerce real-provider smoke' {
      pwsh -NoProfile -File scripts/smoke-commerce-real-provider.ps1 -ConfirmProviderSpend
    }
  }
  if (-not $SkipImageBuild) {
    Invoke-ReleaseStep 'Application image build' {
      docker compose -f $resolvedComposeFile --profile app build
      $releaseEvidence.artifacts.images = @(Get-PreparedImageIdentities)
      Write-ReleaseEvidence -State $releaseEvidence -Path $resolvedEvidencePath
    }
  }

  Complete-ReleaseEvidence `
    -State $releaseEvidence `
    -EvidencePath $resolvedEvidencePath `
    -Status 'passed'
  Write-Host "`nRelease checks passed."
  Write-Host "Prepare evidence: $resolvedEvidencePath"
} catch {
  $releaseFailure = $_
  if ($null -ne $releaseEvidence) {
    Complete-ReleaseEvidence `
      -State $releaseEvidence `
      -EvidencePath $resolvedEvidencePath `
      -Status 'failed' `
      -ErrorRecord $releaseFailure
  }
  throw
} finally {
  Pop-Location
  Remove-Module release-evidence -ErrorAction SilentlyContinue
}
