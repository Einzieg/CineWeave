Set-StrictMode -Version Latest

$script:Utf8NoBom = [System.Text.UTF8Encoding]::new($false)

function Get-ReleaseErrorSummary {
  param([Parameter(Mandatory = $true)][System.Management.Automation.ErrorRecord]$ErrorRecord)

  $summary = [string]$ErrorRecord.Exception.Message
  $summary = ($summary -replace '\s+', ' ').Trim()
  if ($summary.Length -gt 500) {
    $summary = $summary.Substring(0, 500)
  }
  return $summary
}

function Write-ReleaseEvidence {
  param(
    [Parameter(Mandatory = $true)][object]$State,
    [Parameter(Mandatory = $true)][string]$Path
  )

  if ([string]::IsNullOrWhiteSpace($Path)) {
    return
  }
  $resolvedPath = [IO.Path]::GetFullPath($Path)
  $parent = Split-Path -Parent $resolvedPath
  if (-not (Test-Path -LiteralPath $parent)) {
    New-Item -ItemType Directory -Path $parent -Force | Out-Null
  }
  $temporaryPath = "$resolvedPath.$PID.tmp"
  try {
    $json = $State | ConvertTo-Json -Depth 8
    [IO.File]::WriteAllText(
      $temporaryPath,
      $json + [Environment]::NewLine,
      $script:Utf8NoBom
    )
    Move-Item -LiteralPath $temporaryPath -Destination $resolvedPath -Force
  } finally {
    if (Test-Path -LiteralPath $temporaryPath) {
      Remove-Item -LiteralPath $temporaryPath -Force
    }
  }
}

function New-ReleaseEvidenceState {
  param(
    [Parameter(Mandatory = $true)][string]$Phase,
    [Parameter(Mandatory = $true)][string]$ReleaseId,
    [Parameter(Mandatory = $true)][string]$CoreCommit,
    [Parameter(Mandatory = $true)][bool]$RepositoryClean,
    [hashtable]$Options = @{}
  )

  return [pscustomobject]@{
    schemaVersion = 'cineweave.release-evidence.v1'
    phase = $Phase
    releaseId = $ReleaseId
    coreCommit = $CoreCommit
    repositoryClean = $RepositoryClean
    startedAtUtc = [DateTimeOffset]::UtcNow.ToString('O')
    completedAtUtc = $null
    durationMs = $null
    status = 'running'
    errorSummary = $null
    options = [pscustomobject]$Options
    artifacts = [pscustomobject]@{
      images = @()
    }
    steps = [System.Collections.ArrayList]::new()
  }
}

function Invoke-ReleaseEvidenceStep {
  param(
    [Parameter(Mandatory = $true)][object]$State,
    [Parameter(Mandatory = $true)][string]$EvidencePath,
    [Parameter(Mandatory = $true)][string]$Name,
    [Parameter(Mandatory = $true)][scriptblock]$Action
  )

  $step = [pscustomobject]@{
    name = $Name
    startedAtUtc = [DateTimeOffset]::UtcNow.ToString('O')
    completedAtUtc = $null
    durationMs = $null
    status = 'running'
    errorSummary = $null
  }
  [void]$State.steps.Add($step)
  Write-ReleaseEvidence -State $State -Path $EvidencePath
  $stopwatch = [Diagnostics.Stopwatch]::StartNew()
  try {
    & $Action
    $step.status = 'passed'
  } catch {
    $step.status = 'failed'
    $step.errorSummary = Get-ReleaseErrorSummary -ErrorRecord $_
    throw
  } finally {
    $stopwatch.Stop()
    $step.completedAtUtc = [DateTimeOffset]::UtcNow.ToString('O')
    $step.durationMs = $stopwatch.ElapsedMilliseconds
    Write-ReleaseEvidence -State $State -Path $EvidencePath
  }
}

function Complete-ReleaseEvidence {
  param(
    [Parameter(Mandatory = $true)][object]$State,
    [Parameter(Mandatory = $true)][string]$EvidencePath,
    [Parameter(Mandatory = $true)][ValidateSet('passed', 'failed')][string]$Status,
    [System.Management.Automation.ErrorRecord]$ErrorRecord
  )

  $completed = [DateTimeOffset]::UtcNow
  $started = [DateTimeOffset]::Parse($State.startedAtUtc)
  $State.completedAtUtc = $completed.ToString('O')
  $State.durationMs = [int64][Math]::Round(($completed - $started).TotalMilliseconds)
  $State.status = $Status
  if ($null -ne $ErrorRecord) {
    $State.errorSummary = Get-ReleaseErrorSummary -ErrorRecord $ErrorRecord
  }
  Write-ReleaseEvidence -State $State -Path $EvidencePath
}

function Assert-ReleasePrepareEvidence {
  param(
    [Parameter(Mandatory = $true)][string]$Path,
    [Parameter(Mandatory = $true)][string]$ExpectedCoreCommit,
    [string]$ExpectedReleaseId = '',
    [string[]]$RequiredSteps = @(),
    [ValidateRange(1, 168)][int]$MaxAgeHours = 24
  )

  $resolvedPath = [IO.Path]::GetFullPath($Path)
  if (-not (Test-Path -LiteralPath $resolvedPath -PathType Leaf)) {
    throw "Prepare evidence is missing: $resolvedPath"
  }
  $evidence = Get-Content -LiteralPath $resolvedPath -Raw -Encoding UTF8 |
    ConvertFrom-Json
  if (
    $evidence.schemaVersion -ne 'cineweave.release-evidence.v1' -or
    $evidence.phase -ne 'prepare' -or
    $evidence.status -ne 'passed'
  ) {
    throw 'Prepare evidence is not a completed successful prepare checkpoint.'
  }
  if ($evidence.coreCommit -ne $ExpectedCoreCommit) {
    throw "Prepare evidence Core commit is $($evidence.coreCommit); expected $ExpectedCoreCommit."
  }
  if (
    -not [string]::IsNullOrWhiteSpace($ExpectedReleaseId) -and
    $evidence.releaseId -ne $ExpectedReleaseId
  ) {
    throw "Prepare evidence release ID is $($evidence.releaseId); expected $ExpectedReleaseId."
  }
  if ($evidence.repositoryClean -ne $true) {
    throw 'Prepare evidence was not created from a clean repository.'
  }
  $artifactProperty = $evidence.PSObject.Properties['artifacts']
  if ($null -eq $artifactProperty) {
    throw 'Prepare evidence does not contain prepared image identities.'
  }
  $imageProperty = $evidence.artifacts.PSObject.Properties['images']
  $images = @()
  if ($null -ne $imageProperty -and $null -ne $imageProperty.Value) {
    $images = @($evidence.artifacts.images)
  }
  if ($images.Count -eq 0) {
    throw 'Prepare evidence does not contain prepared image identities.'
  }
  $imageNames = [Collections.Generic.HashSet[string]]::new([StringComparer]::Ordinal)
  foreach ($preparedImage in $images) {
    $name = [string]$preparedImage.name
    $imageId = [string]$preparedImage.imageId
    if (
      [string]::IsNullOrWhiteSpace($name) -or
      $imageId -notmatch '^sha256:[0-9a-f]{64}$'
    ) {
      throw 'Prepare evidence contains an invalid prepared image identity.'
    }
    if (-not $imageNames.Add($name)) {
      throw "Prepare evidence contains duplicate image identity: $name"
    }
  }
  $completed = [DateTimeOffset]::Parse([string]$evidence.completedAtUtc)
  if ([DateTimeOffset]::UtcNow - $completed -gt [TimeSpan]::FromHours($MaxAgeHours)) {
    throw "Prepare evidence is older than $MaxAgeHours hours."
  }
  foreach ($requiredStep in $RequiredSteps) {
    $matches = @($evidence.steps | Where-Object {
      $_.name -eq $requiredStep -and $_.status -eq 'passed'
    })
    if ($matches.Count -ne 1) {
      throw "Prepare evidence is missing successful step: $requiredStep"
    }
  }
  return $evidence
}

Export-ModuleMember -Function @(
  'New-ReleaseEvidenceState',
  'Write-ReleaseEvidence',
  'Invoke-ReleaseEvidenceStep',
  'Complete-ReleaseEvidence',
  'Assert-ReleasePrepareEvidence'
)
