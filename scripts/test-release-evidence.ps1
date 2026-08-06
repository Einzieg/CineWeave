[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true
Set-StrictMode -Version Latest

$repoRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
$modulePath = Join-Path $repoRoot 'scripts/release-evidence.psm1'
$releaseCheckPath = Join-Path $repoRoot 'scripts/release-check.ps1'
$temporaryRoot = Join-Path (
  [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
) ('cineweave-release-evidence-' + [Guid]::NewGuid().ToString('N'))

Import-Module $modulePath -Force
New-Item -ItemType Directory -Path $temporaryRoot | Out-Null
try {
  $releaseCheckSource = Get-Content -LiteralPath $releaseCheckPath -Raw -Encoding UTF8
  foreach ($required in @(
    '[string]$ComposeFile',
    'Get-PreparedImageIdentities',
    '$releaseEvidence.artifacts.images',
    "'Application image build'"
  )) {
    if (-not $releaseCheckSource.Contains($required, [StringComparison]::Ordinal)) {
      throw "Release check is missing the Prepare evidence integration: $required"
    }
  }

  $passedPath = Join-Path $temporaryRoot 'prepare-passed.json'
  $passed = New-ReleaseEvidenceState `
    -Phase 'prepare' `
    -ReleaseId 'cw-test-release' `
    -CoreCommit ('a' * 40) `
    -RepositoryClean $true `
    -Options @{ imageBuild = $true }
  Invoke-ReleaseEvidenceStep `
    -State $passed `
    -EvidencePath $passedPath `
    -Name 'Repository validation' `
    -Action { Write-Output 'fixture validation passed' }
  Invoke-ReleaseEvidenceStep `
    -State $passed `
    -EvidencePath $passedPath `
    -Name 'Application image build' `
    -Action { Write-Output 'fixture image build passed' }
  Complete-ReleaseEvidence `
    -State $passed `
    -EvidencePath $passedPath `
    -Status 'passed'
  $passed.artifacts.images = @(
    [pscustomobject]@{
      name = 'cineweave-fixture'
      imageId = 'sha256:' + ('d' * 64)
    }
  )
  Write-ReleaseEvidence -State $passed -Path $passedPath

  $validated = Assert-ReleasePrepareEvidence `
    -Path $passedPath `
    -ExpectedCoreCommit ('a' * 40) `
    -ExpectedReleaseId 'cw-test-release' `
    -RequiredSteps @('Repository validation', 'Application image build')
  if (
    $validated.status -ne 'passed' -or
    @($validated.steps).Count -ne 2 -or
    @($validated.steps | Where-Object { $_.durationMs -ge 0 }).Count -ne 2
  ) {
    throw 'Successful prepare evidence did not preserve step timings.'
  }

  $mismatchRejected = $false
  try {
    Assert-ReleasePrepareEvidence `
      -Path $passedPath `
      -ExpectedCoreCommit ('b' * 40) `
      -ExpectedReleaseId 'cw-test-release' `
      -RequiredSteps @('Repository validation') | Out-Null
  } catch {
    $mismatchRejected = $_.Exception.Message -like '*Core commit*'
  }
  if (-not $mismatchRejected) {
    throw 'Prepare evidence accepted a different Core commit.'
  }

  $missingStepRejected = $false
  try {
    Assert-ReleasePrepareEvidence `
      -Path $passedPath `
      -ExpectedCoreCommit ('a' * 40) `
      -ExpectedReleaseId 'cw-test-release' `
      -RequiredSteps @('Missing fixture') | Out-Null
  } catch {
    $missingStepRejected = $_.Exception.Message -like '*missing successful step*'
  }
  if (-not $missingStepRejected) {
    throw 'Prepare evidence accepted a missing required step.'
  }

  $passed.completedAtUtc = [DateTimeOffset]::UtcNow.AddHours(-2).ToString('O')
  Write-ReleaseEvidence -State $passed -Path $passedPath
  $staleRejected = $false
  try {
    Assert-ReleasePrepareEvidence `
      -Path $passedPath `
      -ExpectedCoreCommit ('a' * 40) `
      -ExpectedReleaseId 'cw-test-release' `
      -MaxAgeHours 1 | Out-Null
  } catch {
    $staleRejected = $_.Exception.Message -like '*older than*'
  }
  if (-not $staleRejected) {
    throw 'Prepare evidence accepted a stale checkpoint.'
  }

  $noImagesPath = Join-Path $temporaryRoot 'prepare-no-images.json'
  $noImages = New-ReleaseEvidenceState `
    -Phase 'prepare' `
    -ReleaseId 'cw-no-images' `
    -CoreCommit ('e' * 40) `
    -RepositoryClean $true
  Complete-ReleaseEvidence `
    -State $noImages `
    -EvidencePath $noImagesPath `
    -Status 'passed'
  $noImagesRejected = $false
  $noImagesError = ''
  try {
    Assert-ReleasePrepareEvidence `
      -Path $noImagesPath `
      -ExpectedCoreCommit ('e' * 40) `
      -ExpectedReleaseId 'cw-no-images' | Out-Null
  } catch {
    $noImagesError = $_.Exception.Message
    $noImagesRejected = $_.Exception.Message -like '*prepared image identities*'
  }
  if (-not $noImagesRejected) {
    throw "Prepare evidence accepted a checkpoint without image identities. Actual error: $noImagesError"
  }

  $failedPath = Join-Path $temporaryRoot 'prepare-failed.json'
  $failed = New-ReleaseEvidenceState `
    -Phase 'prepare' `
    -ReleaseId 'cw-failed-release' `
    -CoreCommit ('c' * 40) `
    -RepositoryClean $true
  $failure = $null
  try {
    Invoke-ReleaseEvidenceStep `
      -State $failed `
      -EvidencePath $failedPath `
      -Name 'Failing fixture' `
      -Action { throw 'fixture failure' }
  } catch {
    $failure = $_
  }
  if ($null -eq $failure) {
    throw 'Failing release step did not propagate its error.'
  }
  Complete-ReleaseEvidence `
    -State $failed `
    -EvidencePath $failedPath `
    -Status 'failed' `
    -ErrorRecord $failure
  $failedJSON = Get-Content -LiteralPath $failedPath -Raw -Encoding UTF8 |
    ConvertFrom-Json
  if (
    $failedJSON.status -ne 'failed' -or
    $failedJSON.steps[0].status -ne 'failed' -or
    [string]::IsNullOrWhiteSpace([string]$failedJSON.errorSummary)
  ) {
    throw 'Failed prepare evidence did not preserve the terminal failure.'
  }

  Write-Host 'Release evidence contract passed.'
} finally {
  Remove-Module release-evidence -ErrorAction SilentlyContinue
  if (Test-Path -LiteralPath $temporaryRoot) {
    Remove-Item -LiteralPath $temporaryRoot -Recurse -Force
  }
}
