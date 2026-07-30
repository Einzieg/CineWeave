[CmdletBinding()]
param(
  [Parameter(Mandatory = $true)]
  [string]$CommercialRepositoryPath,

  [Parameter(Mandatory = $true)]
  [string]$CommercialCommit,

  [Parameter(Mandatory = $true)]
  [string]$OutputDirectory,

  [string]$CoreRepositoryPath = '',
  [string]$CoreLockRelativePath = 'core.lock',
  [string]$OverlayAllowlistRelativePath = 'overlay-allowlist.v1.json'
)

$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true

$utf8NoBom = [System.Text.UTF8Encoding]::new($false)
[Console]::OutputEncoding = $utf8NoBom
$OutputEncoding = $utf8NoBom

$repoRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
if ([string]::IsNullOrWhiteSpace($CoreRepositoryPath)) {
  $CoreRepositoryPath = $repoRoot
}
$coreRoot = (Resolve-Path -LiteralPath $CoreRepositoryPath).Path
$commercialRoot = (Resolve-Path -LiteralPath $CommercialRepositoryPath).Path
$outputRoot = [IO.Path]::GetFullPath($OutputDirectory)
$assemblyScriptRelativePath = [IO.Path]::GetRelativePath(
  $repoRoot,
  $PSCommandPath
).Replace([string][IO.Path]::DirectorySeparatorChar, '/')

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

if ($outputRoot -eq $coreRoot -or (Test-PathWithin -Candidate $outputRoot -Parent $coreRoot)) {
  throw 'Assembly output must be outside the Core repository.'
}
if ($outputRoot -eq $commercialRoot -or (Test-PathWithin -Candidate $outputRoot -Parent $commercialRoot)) {
  throw 'Assembly output must be outside the Commercial repository.'
}
if (Test-Path -LiteralPath $outputRoot) {
  throw "Assembly output already exists: $outputRoot"
}

$coreLockPath = Join-Path $commercialRoot $CoreLockRelativePath
$overlayPath = Join-Path $commercialRoot $OverlayAllowlistRelativePath
if (-not (Test-Path -LiteralPath $coreLockPath -PathType Leaf)) {
  throw "Core lock is missing: $coreLockPath"
}
if (-not (Test-Path -LiteralPath $overlayPath -PathType Leaf)) {
  throw "Overlay allowlist is missing: $overlayPath"
}

$checker = Join-Path $repoRoot 'scripts/check-commercial-assembly-contract.py'
& python $checker `
  --core-lock $coreLockPath `
  --overlay $overlayPath `
  --core-root $coreRoot `
  --overlay-root $commercialRoot `
  --commercial-commit $CommercialCommit
if ($LASTEXITCODE -ne 0) {
  throw "Commercial Assembly contract check failed with exit code $LASTEXITCODE"
}

$coreLock = Get-Content -LiteralPath $coreLockPath -Raw -Encoding UTF8 | ConvertFrom-Json
$overlay = Get-Content -LiteralPath $overlayPath -Raw -Encoding UTF8 | ConvertFrom-Json
$temporaryParent = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
$temporaryRoot = Join-Path $temporaryParent ("cineweave-commercial-assembly-" + [Guid]::NewGuid().ToString('N'))
$coreArchivePath = Join-Path $temporaryRoot 'core.tar'
$commercialArchivePath = Join-Path $temporaryRoot 'commercial.tar'
$commercialArchiveRoot = Join-Path $temporaryRoot 'commercial'

New-Item -ItemType Directory -Path $temporaryRoot | Out-Null
try {
  New-Item -ItemType Directory -Path $outputRoot | Out-Null
  & git `
    -c core.autocrlf=false `
    -c core.eol=lf `
    -C $coreRoot `
    archive `
    --format=tar `
    --output=$coreArchivePath `
    $coreLock.coreCommit
  if ($LASTEXITCODE -ne 0) {
    throw "Core archive failed with exit code $LASTEXITCODE"
  }
  & tar -xf $coreArchivePath -C $outputRoot
  if ($LASTEXITCODE -ne 0) {
    throw "Core archive extraction failed with exit code $LASTEXITCODE"
  }
  & git `
    -c core.autocrlf=false `
    -c core.eol=lf `
    -C $commercialRoot `
    archive `
    --format=tar `
    --output=$commercialArchivePath `
    $CommercialCommit
  if ($LASTEXITCODE -ne 0) {
    throw "Commercial archive failed with exit code $LASTEXITCODE"
  }
  New-Item -ItemType Directory -Path $commercialArchiveRoot | Out-Null
  & tar -xf $commercialArchivePath -C $commercialArchiveRoot
  if ($LASTEXITCODE -ne 0) {
    throw "Commercial archive extraction failed with exit code $LASTEXITCODE"
  }

  $archivedCoreLockPath = Join-Path $commercialArchiveRoot $CoreLockRelativePath
  $archivedOverlayPath = Join-Path $commercialArchiveRoot $OverlayAllowlistRelativePath
  $coreLock = Get-Content -LiteralPath $archivedCoreLockPath -Raw -Encoding UTF8 |
    ConvertFrom-Json
  $overlay = Get-Content -LiteralPath $archivedOverlayPath -Raw -Encoding UTF8 |
    ConvertFrom-Json

  $assembledFiles = [System.Collections.Generic.List[object]]::new()
  foreach ($mapping in $overlay.files) {
    $sourcePath = [IO.Path]::GetFullPath(
      (Join-Path $commercialArchiveRoot ($mapping.source -replace '/', [IO.Path]::DirectorySeparatorChar))
    )
    $destinationPath = [IO.Path]::GetFullPath(
      (Join-Path $outputRoot ($mapping.destination -replace '/', [IO.Path]::DirectorySeparatorChar))
    )
    if (-not (Test-PathWithin -Candidate $sourcePath -Parent $commercialArchiveRoot)) {
      throw "Overlay source escaped the Commercial repository: $($mapping.source)"
    }
    if (-not (Test-PathWithin -Candidate $destinationPath -Parent $outputRoot)) {
      throw "Overlay destination escaped the assembly tree: $($mapping.destination)"
    }
    $destinationParent = Split-Path -Parent $destinationPath
    if (-not (Test-Path -LiteralPath $destinationParent)) {
      New-Item -ItemType Directory -Path $destinationParent | Out-Null
    }
    Copy-Item -LiteralPath $sourcePath -Destination $destinationPath
    $actualHash = (Get-FileHash -LiteralPath $destinationPath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actualHash -ne $mapping.sha256) {
      throw "Overlay output hash drifted after copy: $($mapping.destination)"
    }
    $assembledFiles.Add([ordered]@{
      source = $mapping.source
      destination = $mapping.destination
      operation = $mapping.operation
      sha256 = $actualHash
    })
  }

  $evidenceDirectory = Join-Path $outputRoot '.cineweave'
  New-Item -ItemType Directory -Path $evidenceDirectory | Out-Null
  $evidence = [ordered]@{
    schemaVersion = 'cineweave.assembly-inputs.v1'
    coreCommit = $coreLock.coreCommit
    commercialAssemblyCommit = $CommercialCommit
    coreLockSha256 = (Get-FileHash -LiteralPath $archivedCoreLockPath -Algorithm SHA256).Hash.ToLowerInvariant()
    overlayAllowlistSha256 = (Get-FileHash -LiteralPath $archivedOverlayPath -Algorithm SHA256).Hash.ToLowerInvariant()
    overlaySlotsSha256 = $coreLock.overlaySlotsSha256
    assemblyScriptPath = $assemblyScriptRelativePath
    assemblyScriptSha256 = (
      Get-FileHash `
        -LiteralPath (Join-Path $outputRoot $assemblyScriptRelativePath) `
        -Algorithm SHA256
    ).Hash.ToLowerInvariant()
    cleanCoreTree = $true
    cleanCommercialTree = $true
    assembledAt = [DateTimeOffset]::UtcNow.ToString('O')
    files = $assembledFiles
  }
  $evidenceJSON = $evidence | ConvertTo-Json -Depth 8
  [IO.File]::WriteAllText(
    (Join-Path $evidenceDirectory 'assembly-inputs.json'),
    $evidenceJSON + [Environment]::NewLine,
    $utf8NoBom
  )
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

Write-Host "Commercial Assembly created: $outputRoot"
Write-Host "Core commit: $($coreLock.coreCommit)"
Write-Host "Commercial commit: $CommercialCommit"
Write-Host "Overlay files: $($overlay.files.Count)"
