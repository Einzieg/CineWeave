param(
  [Parameter(Mandatory = $true)]
  [string]$ContainerName,

  [Parameter(Mandatory = $true)]
  [string]$OutputPath,

  [Parameter(Mandatory = $true)]
  [string]$UpstreamEvidencePath,

  [Parameter(Mandatory = $true)]
  [string]$ContractManifestPath,

  [Parameter(Mandatory = $true)]
  [string]$ReleaseManifestPath,

  [string]$CommercialRepository = '..\CineWeave-Commercial'
)

$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true
Set-StrictMode -Version Latest

$repositoryRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$commercialCandidate = [IO.Path]::GetFullPath((Join-Path $repositoryRoot $CommercialRepository))
$resolvedOutput = [IO.Path]::GetFullPath($OutputPath)
$outputParent = Split-Path -Parent $resolvedOutput

if ([string]::IsNullOrWhiteSpace($outputParent)) {
  throw 'OutputPath must have a parent directory.'
}

$sourceRoots = @($repositoryRoot)
if (Test-Path -LiteralPath $commercialCandidate -PathType Container) {
  $sourceRoots += (Resolve-Path -LiteralPath $commercialCandidate).Path
}

foreach ($sourceRoot in $sourceRoots) {
  $rootWithSeparator = $sourceRoot.TrimEnd('\', '/') + [IO.Path]::DirectorySeparatorChar
  if (
    $resolvedOutput.Equals($sourceRoot, [StringComparison]::OrdinalIgnoreCase) -or
    $resolvedOutput.StartsWith($rootWithSeparator, [StringComparison]::OrdinalIgnoreCase)
  ) {
    throw 'New API runtime evidence must be stored outside both source repositories.'
  }
}

New-Item -ItemType Directory -Path $outputParent -Force | Out-Null

$containerJson = & docker inspect $ContainerName
if ($LASTEXITCODE -ne 0) {
  throw "docker inspect failed for New API container '$ContainerName'."
}
$containerRows = @($containerJson | ConvertFrom-Json)
if ($containerRows.Count -ne 1) {
  throw "Expected exactly one New API container, found $($containerRows.Count)."
}
$container = $containerRows[0]

$imageJson = & docker image inspect ([string]$container.Image)
if ($LASTEXITCODE -ne 0) {
  throw "docker image inspect failed for New API image '$($container.Image)'."
}
$imageRows = @($imageJson | ConvertFrom-Json)
if ($imageRows.Count -ne 1) {
  throw "Expected exactly one New API image, found $($imageRows.Count)."
}
$image = $imageRows[0]

$document = [ordered]@{
  schemaVersion = 'cineweave.new-api-runtime-image.v1'
  capturedAt = [DateTimeOffset]::UtcNow.ToString('o')
  container = [ordered]@{
    name = [string]$container.Name
    configuredImageReference = [string]$container.Config.Image
    imageId = [string]$container.Image
  }
  image = [ordered]@{
    repoDigests = @($image.RepoDigests | ForEach-Object { [string]$_ })
  }
}

$document |
  ConvertTo-Json -Depth 8 |
  Set-Content -LiteralPath $resolvedOutput -Encoding UTF8

& python (Join-Path $PSScriptRoot 'check-new-api-runtime-image.py') `
  --runtime-evidence $resolvedOutput `
  --upstream-evidence $UpstreamEvidencePath `
  --contract-manifest $ContractManifestPath `
  --release-manifest $ReleaseManifestPath
if ($LASTEXITCODE -ne 0) {
  throw 'New API runtime image evidence did not pass the immutable release gate.'
}

Write-Host "New API runtime image evidence saved: $resolvedOutput"
