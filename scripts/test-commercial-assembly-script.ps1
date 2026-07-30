$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true

$utf8NoBom = [System.Text.UTF8Encoding]::new($false)
[Console]::OutputEncoding = $utf8NoBom
$OutputEncoding = $utf8NoBom

$repoRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
$temporaryParent = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
$temporaryRoot = Join-Path $temporaryParent ("cineweave-assembly-test-" + [Guid]::NewGuid().ToString('N'))
$coreRoot = Join-Path $temporaryRoot 'core'
$commercialRoot = Join-Path $temporaryRoot 'commercial'
$outputRoot = Join-Path $temporaryRoot 'assembled'

function Write-Utf8File {
  param(
    [Parameter(Mandatory = $true)]
    [string]$Path,
    [Parameter(Mandatory = $true)]
    [string]$Content
  )

  $parent = Split-Path -Parent $Path
  if (-not (Test-Path -LiteralPath $parent)) {
    New-Item -ItemType Directory -Path $parent | Out-Null
  }
  [IO.File]::WriteAllText($Path, $Content, $utf8NoBom)
}

function Invoke-Git {
  param(
    [Parameter(Mandatory = $true)]
    [string]$Repository,
    [Parameter(Mandatory = $true)]
    [string[]]$Arguments
  )

  & git -C $Repository @Arguments
  if ($LASTEXITCODE -ne 0) {
    throw "git $($Arguments -join ' ') failed with exit code $LASTEXITCODE"
  }
}

function Get-LowerSHA256 {
  param([Parameter(Mandatory = $true)][string]$Path)
  return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
}

function Get-GitBlobSHA256 {
  param(
    [Parameter(Mandatory = $true)]
    [string]$Repository,
    [Parameter(Mandatory = $true)]
    [string]$Revision,
    [Parameter(Mandatory = $true)]
    [string]$Path
  )

  $digest = & python -c @'
import hashlib
import subprocess
import sys

repository, revision, path = sys.argv[1:]
result = subprocess.run(
    ["git", "-C", repository, "show", f"{revision}:{path}"],
    check=True,
    stdout=subprocess.PIPE,
)
print(hashlib.sha256(result.stdout).hexdigest())
'@ $Repository $Revision ($Path.Replace('\', '/'))
  if ($LASTEXITCODE -ne 0) {
    throw "Unable to hash Git blob $Revision`:$Path"
  }
  return ([string]$digest).Trim()
}

function Test-PathWithin {
  param(
    [Parameter(Mandatory = $true)]
    [string]$Candidate,
    [Parameter(Mandatory = $true)]
    [string]$Parent
  )
  $candidateFull = [IO.Path]::GetFullPath($Candidate)
  $parentFull = [IO.Path]::GetFullPath($Parent).TrimEnd(
    [IO.Path]::DirectorySeparatorChar,
    [IO.Path]::AltDirectorySeparatorChar
  )
  return $candidateFull.StartsWith(
    $parentFull + [IO.Path]::DirectorySeparatorChar,
    [StringComparison]::OrdinalIgnoreCase
  )
}

New-Item -ItemType Directory -Path $coreRoot, $commercialRoot | Out-Null
try {
  $coreFiles = @(
    'packages/edition/edition.v2.json',
    'packages/edition/ddl-owners.v1.json',
    'packages/edition/overlay-slots.v1.json',
    'packages/edition/release-manifest.schema.json',
    'packages/openapi/openapi.yaml',
    'packages/events/catalog.yaml',
    'scripts/assemble-commercial-release.ps1'
  )
  foreach ($relative in $coreFiles) {
    $destination = Join-Path $coreRoot ($relative -replace '/', [IO.Path]::DirectorySeparatorChar)
    $parent = Split-Path -Parent $destination
    if (-not (Test-Path -LiteralPath $parent)) {
      New-Item -ItemType Directory -Path $parent | Out-Null
    }
    Copy-Item -LiteralPath (Join-Path $repoRoot $relative) -Destination $destination
  }
  Write-Utf8File `
    -Path (Join-Path $coreRoot 'db/migrations/000001_fixture.sql') `
    -Content "-- +goose Up`nSELECT 1;`n-- +goose Down`nSELECT 1;`n"
  Write-Utf8File `
    -Path (Join-Path $coreRoot 'apps/web/src/edition/selected-entry.ts') `
    -Content "export const selectedEditionEntry = 'community';`n"
  Write-Utf8File -Path (Join-Path $coreRoot 'README.md') -Content "fixture core`n"

  Invoke-Git -Repository $coreRoot -Arguments @('init', '--quiet')
  Invoke-Git -Repository $coreRoot -Arguments @('config', 'user.name', 'CineWeave Assembly Test')
  Invoke-Git -Repository $coreRoot -Arguments @('config', 'user.email', 'assembly-test@example.invalid')
  Invoke-Git -Repository $coreRoot -Arguments @('remote', 'add', 'origin', 'https://example.invalid/cineweave-core.git')
  Invoke-Git -Repository $coreRoot -Arguments @('add', '--all')
  Invoke-Git -Repository $coreRoot -Arguments @('commit', '--quiet', '-m', 'fixture core')
  $coreCommit = (& git -C $coreRoot rev-parse HEAD).Trim()

  Write-Utf8File `
    -Path (Join-Path $commercialRoot 'overlay/fixture.txt') `
    -Content "commercial add fixture`n"
  Write-Utf8File `
    -Path (Join-Path $commercialRoot 'overlay/selected-entry.ts') `
    -Content "export const selectedEditionEntry = 'commercial';`n"

  Invoke-Git -Repository $commercialRoot -Arguments @('init', '--quiet')
  Invoke-Git -Repository $commercialRoot -Arguments @('config', 'user.name', 'CineWeave Assembly Test')
  Invoke-Git -Repository $commercialRoot -Arguments @('config', 'user.email', 'assembly-test@example.invalid')
  Invoke-Git -Repository $commercialRoot -Arguments @('add', 'overlay')
  Invoke-Git -Repository $commercialRoot -Arguments @('commit', '--quiet', '-m', 'fixture commercial sources')
  $commercialSourcesCommit = (& git -C $commercialRoot rev-parse HEAD).Trim()

  $coreLock = [ordered]@{
    schemaVersion = 'cineweave.core-lock.v1'
    coreRepository = 'https://example.invalid/cineweave-core.git'
    coreCommit = $coreCommit
    editionContractSha256 = (Get-GitBlobSHA256 -Repository $coreRoot -Revision $coreCommit -Path 'packages/edition/edition.v2.json')
    ddlOwnerManifestSha256 = (Get-GitBlobSHA256 -Repository $coreRoot -Revision $coreCommit -Path 'packages/edition/ddl-owners.v1.json')
    overlaySlotsSha256 = (Get-GitBlobSHA256 -Repository $coreRoot -Revision $coreCommit -Path 'packages/edition/overlay-slots.v1.json')
    releaseManifestSchemaSha256 = (Get-GitBlobSHA256 -Repository $coreRoot -Revision $coreCommit -Path 'packages/edition/release-manifest.schema.json')
    openAPIContractSha256 = (Get-GitBlobSHA256 -Repository $coreRoot -Revision $coreCommit -Path 'packages/openapi/openapi.yaml')
    eventCatalogSha256 = (Get-GitBlobSHA256 -Repository $coreRoot -Revision $coreCommit -Path 'packages/events/catalog.yaml')
    coreMigrationHead = 1
  }
  $overlay = [ordered]@{
    schemaVersion = 'cineweave.overlay-allowlist.v1'
    coreCommit = $coreCommit
    files = @(
      [ordered]@{
        source = 'overlay/fixture.txt'
        destination = 'assembly/fixture.txt'
        operation = 'add'
        sha256 = (Get-GitBlobSHA256 -Repository $commercialRoot -Revision $commercialSourcesCommit -Path 'overlay/fixture.txt')
      },
      [ordered]@{
        source = 'overlay/selected-entry.ts'
        destination = 'apps/web/src/edition/selected-entry.ts'
        operation = 'replace'
        sha256 = (Get-GitBlobSHA256 -Repository $commercialRoot -Revision $commercialSourcesCommit -Path 'overlay/selected-entry.ts')
      }
    )
  }
  Write-Utf8File `
    -Path (Join-Path $commercialRoot 'core.lock') `
    -Content (($coreLock | ConvertTo-Json -Depth 6) + [Environment]::NewLine)
  Write-Utf8File `
    -Path (Join-Path $commercialRoot 'overlay-allowlist.v1.json') `
    -Content (($overlay | ConvertTo-Json -Depth 6) + [Environment]::NewLine)

  Invoke-Git -Repository $commercialRoot -Arguments @('add', '--all')
  Invoke-Git -Repository $commercialRoot -Arguments @('commit', '--quiet', '-m', 'fixture commercial contract')
  $commercialCommit = (& git -C $commercialRoot rev-parse HEAD).Trim()

  & (Join-Path $repoRoot 'scripts/assemble-commercial-release.ps1') `
    -CommercialRepositoryPath $commercialRoot `
    -CommercialCommit $commercialCommit `
    -OutputDirectory $outputRoot `
    -CoreRepositoryPath $coreRoot
  if ($LASTEXITCODE -ne 0) {
    throw "Assembly script failed with exit code $LASTEXITCODE"
  }

  $added = Get-Content -LiteralPath (Join-Path $outputRoot 'assembly/fixture.txt') -Raw -Encoding UTF8
  if ($added.Trim() -ne 'commercial add fixture') {
    throw 'Assembled add file is incorrect.'
  }
  $replaced = Get-Content -LiteralPath (Join-Path $outputRoot 'apps/web/src/edition/selected-entry.ts') -Raw -Encoding UTF8
  if ($replaced -notmatch "'commercial'") {
    throw 'Assembled replacement slot is incorrect.'
  }
  $evidence = Get-Content -LiteralPath (Join-Path $outputRoot '.cineweave/assembly-inputs.json') -Raw -Encoding UTF8 | ConvertFrom-Json
  if ($evidence.coreCommit -ne $coreCommit -or $evidence.commercialAssemblyCommit -ne $commercialCommit) {
    throw 'Assembly evidence does not bind both commits.'
  }
  if (
    $evidence.coreLockSha256 -ne (Get-GitBlobSHA256 -Repository $commercialRoot -Revision $commercialCommit -Path 'core.lock') -or
    $evidence.overlayAllowlistSha256 -ne (Get-GitBlobSHA256 -Repository $commercialRoot -Revision $commercialCommit -Path 'overlay-allowlist.v1.json') -or
    $evidence.assemblyScriptPath -ne 'scripts/assemble-commercial-release.ps1' -or
    $evidence.assemblyScriptSha256 -ne (Get-GitBlobSHA256 -Repository $coreRoot -Revision $coreCommit -Path 'scripts/assemble-commercial-release.ps1')
  ) {
    throw 'Assembly evidence does not bind the immutable input files.'
  }
  if ($evidence.cleanCoreTree -ne $true -or $evidence.cleanCommercialTree -ne $true) {
    throw 'Assembly evidence does not record both clean repository checks.'
  }

  [IO.File]::AppendAllText(
    (Join-Path $commercialRoot 'overlay/fixture.txt'),
    "dirty`n",
    $utf8NoBom
  )
  $rejectedOutput = Join-Path $temporaryRoot 'rejected'
  $rejected = $false
  try {
    & (Join-Path $repoRoot 'scripts/assemble-commercial-release.ps1') `
      -CommercialRepositoryPath $commercialRoot `
      -CommercialCommit $commercialCommit `
      -OutputDirectory $rejectedOutput `
      -CoreRepositoryPath $coreRoot
  }
  catch {
    $rejected = $true
  }
  if (-not $rejected) {
    throw 'Assembly script accepted a dirty Commercial repository.'
  }
  if (Test-Path -LiteralPath $rejectedOutput) {
    throw 'Rejected assembly created an output directory.'
  }

  Write-Host 'Commercial Assembly script integration checks passed.'
}
finally {
  $resolvedTemporary = [IO.Path]::GetFullPath($temporaryRoot)
  if (
    (Test-Path -LiteralPath $resolvedTemporary) -and
    (Test-PathWithin -Candidate $resolvedTemporary -Parent $temporaryParent)
  ) {
    Remove-Item -LiteralPath $resolvedTemporary -Recurse -Force
  }
}
