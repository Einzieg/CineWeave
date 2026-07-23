[CmdletBinding()]
param(
  [string]$ApiBaseUrl = $env:CINEWEAVE_SMOKE_API_BASE_URL,
  [string]$AccessToken = $env:CINEWEAVE_SMOKE_ACCESS_TOKEN,
  [string]$OrganizationId = $env:CINEWEAVE_SMOKE_ORGANIZATION_ID,
  [string]$ProjectId = $env:CINEWEAVE_SMOKE_PROJECT_ID,
  [string]$ScriptUnitId = $env:CINEWEAVE_SMOKE_SCRIPT_UNIT_ID,
  [ValidateSet('reference-prompts', 'reference-images', 'video-prompts', 'shot-videos', 'full')]
  [string]$Stage = 'full',
  [string[]]$ShotIds = @(),
  [ValidateRange(1, 20)]
  [int]$ShotCount = 3,
  [ValidateRange(1, 20)]
  [int]$Concurrency = 3,
  [ValidateRange(30, 14400)]
  [int]$TimeoutSeconds = 3600,
  [ValidateRange(1, 60)]
  [int]$PollSeconds = 5,
  [switch]$Force,
  [switch]$RetryFailedOnce,
  [switch]$RequireNonChineseTargetLanguage,
  [switch]$PreflightOnly,
  [switch]$ConfirmProviderSpend,
  [string]$EvidencePath
)

$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true

$utf8NoBom = [System.Text.UTF8Encoding]::new($false)
[Console]::OutputEncoding = $utf8NoBom
$OutputEncoding = $utf8NoBom

if (-not $PreflightOnly -and -not $ConfirmProviderSpend) {
  throw 'Real Provider smoke can incur charges. Re-run with -ConfirmProviderSpend.'
}

if ([string]::IsNullOrWhiteSpace($ApiBaseUrl)) {
  $ApiBaseUrl = 'http://localhost:19288'
}
$required = @{
  CINEWEAVE_SMOKE_ACCESS_TOKEN = $AccessToken
  CINEWEAVE_SMOKE_ORGANIZATION_ID = $OrganizationId
  CINEWEAVE_SMOKE_PROJECT_ID = $ProjectId
  CINEWEAVE_SMOKE_SCRIPT_UNIT_ID = $ScriptUnitId
}
foreach ($entry in $required.GetEnumerator()) {
  if ([string]::IsNullOrWhiteSpace([string]$entry.Value)) {
    throw "Missing required value. Set $($entry.Key) or pass the corresponding parameter."
  }
}

$requestHeaders = @{
  Accept = 'application/json'
  Authorization = "Bearer $($AccessToken.Trim())"
  'X-Organization-Id' = $OrganizationId.Trim()
}
$terminalStatuses = @('partially_succeeded', 'succeeded', 'failed', 'cancelled')
$evidence = [System.Collections.Generic.List[object]]::new()
$startedAt = [DateTimeOffset]::UtcNow

function Invoke-StudioApi {
  param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('GET', 'POST')]
    [string]$Method,
    [Parameter(Mandatory = $true)]
    [string]$Path,
    [object]$Body,
    [string]$IdempotencyKey
  )

  $headers = @{}
  foreach ($header in $requestHeaders.GetEnumerator()) {
    $headers[$header.Key] = $header.Value
  }
  if (-not [string]::IsNullOrWhiteSpace($IdempotencyKey)) {
    $headers['Idempotency-Key'] = $IdempotencyKey
  }

  $parameters = @{
    Method = $Method
    Uri = "$($ApiBaseUrl.TrimEnd('/'))$Path"
    Headers = $headers
  }
  if ($null -ne $Body) {
    $parameters.ContentType = 'application/json; charset=utf-8'
    $parameters.Body = $Body | ConvertTo-Json -Depth 30 -Compress
  }

  try {
    $response = Invoke-RestMethod @parameters
  } catch {
    $message = $_.Exception.Message
    if (-not [string]::IsNullOrWhiteSpace($_.ErrorDetails.Message)) {
      try {
        $errorEnvelope = $_.ErrorDetails.Message | ConvertFrom-Json
        if ($errorEnvelope.error.message) {
          $message = "$($errorEnvelope.error.code): $($errorEnvelope.error.message)"
        }
      } catch {
        $message = $_.ErrorDetails.Message
      }
    }
    throw "Commerce smoke API $Method $Path failed: $message"
  }

  if ($null -eq $response.data) {
    throw "Commerce smoke API $Method $Path returned no data envelope."
  }
  return $response.data
}

function Wait-CommerceRun {
  param(
    [Parameter(Mandatory = $true)]
    [string]$RunId,
    [Parameter(Mandatory = $true)]
    [string]$Label
  )

  $deadline = [DateTimeOffset]::UtcNow.AddSeconds($TimeoutSeconds)
  while ([DateTimeOffset]::UtcNow -lt $deadline) {
    $detail = Invoke-StudioApi -Method GET -Path "/api/projects/$ProjectId/commerce/production-runs/$RunId"
    $run = $detail.run
    $completed = [int]$run.completedItems + [int]$run.failedItems + [int]$run.cancelledItems
    $percent = if ([int]$run.totalItems -gt 0) { [Math]::Min(100, [Math]::Floor(($completed * 100) / [int]$run.totalItems)) } else { 0 }
    Write-Progress -Activity $Label -Status "$($run.status) $completed/$($run.totalItems)" -PercentComplete $percent
    if ($terminalStatuses -contains [string]$run.status) {
      Write-Progress -Activity $Label -Completed
      return $detail
    }
    Start-Sleep -Seconds $PollSeconds
  }
  Write-Progress -Activity $Label -Completed
  throw "$Label timed out after $TimeoutSeconds seconds (run $RunId)."
}

function Invoke-CommerceRunWithRetry {
  param(
    [Parameter(Mandatory = $true)]
    [object]$Run,
    [Parameter(Mandatory = $true)]
    [string]$Label
  )

  $detail = Wait-CommerceRun -RunId $Run.id -Label $Label
  if ($detail.run.status -ne 'succeeded' -and $RetryFailedOnce) {
    $retryableIds = @($detail.items | Where-Object { $_.retryable -and $_.status -in @('failed_retryable', 'failed_terminal') } | ForEach-Object { $_.id })
    if ($retryableIds.Count -gt 0) {
      $retry = Invoke-StudioApi -Method POST `
        -Path "/api/projects/$ProjectId/commerce/production-runs/$($detail.run.id)/retry-failed" `
        -IdempotencyKey "commerce-smoke-retry-$([Guid]::NewGuid())" `
        -Body @{ itemIds = $retryableIds; concurrency = $Concurrency }
      $detail = Wait-CommerceRun -RunId $retry.id -Label "$Label retry"
    }
  }

  $failedItems = @($detail.items | Where-Object { $_.status -in @('failed_retryable', 'failed_terminal', 'cancelled', 'discarded') })
  if ($detail.run.status -ne 'succeeded' -or $failedItems.Count -gt 0) {
    $failureSummary = @($failedItems | ForEach-Object { "$($_.subject.key):$($_.errorCode):$($_.errorMessage)" }) -join '; '
    throw "$Label finished as $($detail.run.status). $failureSummary"
  }
  return $detail
}

function Add-RunEvidence {
  param(
    [Parameter(Mandatory = $true)]
    [string]$StageName,
    [Parameter(Mandatory = $true)]
    [object]$Detail
  )

  $evidence.Add([pscustomobject]@{
    stage = $StageName
    runId = [string]$Detail.run.id
    workflowRunId = [string]$Detail.run.workflowRunId
    status = [string]$Detail.run.status
    totalItems = [int]$Detail.run.totalItems
    completedItems = [int]$Detail.run.completedItems
    outputArtifactIds = @($Detail.items | Where-Object outputArtifactId | ForEach-Object outputArtifactId)
    outputMediaFileIds = @($Detail.items | Where-Object outputMediaFileId | ForEach-Object outputMediaFileId)
    outputVideoPromptPlanIds = @($Detail.items | Where-Object outputVideoPromptPlanId | ForEach-Object outputVideoPromptPlanId)
    outputVideoRenderPlanIds = @($Detail.items | Where-Object outputVideoRenderPlanId | ForEach-Object outputVideoRenderPlanId)
    providerRequestIds = @($Detail.items | Where-Object providerRequestId | ForEach-Object providerRequestId)
    providerCallIds = @($Detail.items | Where-Object providerCallId | ForEach-Object providerCallId)
    providerAsyncTaskIds = @($Detail.items | Where-Object providerAsyncTaskId | ForEach-Object providerAsyncTaskId)
  })
}

function Write-SmokeEvidence {
  param(
    [Parameter(Mandatory = $true)]
    [string]$PathPrefix,
    [Parameter(Mandatory = $true)]
    [object]$Payload
  )

  if ([string]::IsNullOrWhiteSpace($EvidencePath)) {
    $repoRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
    $script:EvidencePath = Join-Path $repoRoot "tmp/$PathPrefix-$($startedAt.ToString('yyyyMMdd-HHmmss')).json"
  }
  $evidenceFullPath = [System.IO.Path]::GetFullPath($EvidencePath)
  $evidenceDirectory = Split-Path -Parent $evidenceFullPath
  if (-not (Test-Path -LiteralPath $evidenceDirectory)) {
    New-Item -ItemType Directory -Path $evidenceDirectory -Force | Out-Null
  }
  $Payload | ConvertTo-Json -Depth 20 | Set-Content -LiteralPath $evidenceFullPath -Encoding UTF8
  return $evidenceFullPath
}

$project = Invoke-StudioApi -Method GET -Path "/api/projects/$ProjectId"
if ([string]$project.projectKind -ne 'commerce_video') {
  throw "Project $ProjectId is $($project.projectKind), not commerce_video."
}
if ([string]::IsNullOrWhiteSpace([string]$project.workspaceId)) {
  throw "Commerce project $ProjectId has no workspace identity."
}
$projectOptions = Invoke-StudioApi -Method GET -Path "/api/workspaces/$($project.workspaceId)/commerce/project-options"
if (-not [bool]$projectOptions.available) {
  $blockers = @($projectOptions.blockers | ForEach-Object { [string]$_ }) -join '; '
  throw "Commerce project options are unavailable. $blockers"
}

$unitList = Invoke-StudioApi -Method GET -Path "/api/projects/$ProjectId/commerce/script-units?filter%5Bstatus%5D=active&limit=100"
$unit = @($unitList.items | Where-Object { $_.id -eq $ScriptUnitId }) | Select-Object -First 1
if ($null -eq $unit) {
  throw "Active Commerce ScriptUnit $ScriptUnitId was not found in project $ProjectId."
}
if ([string]::IsNullOrWhiteSpace([string]$unit.activeUnitGenerationId)) {
  throw "Commerce ScriptUnit $ScriptUnitId has no active UnitGeneration."
}

$planList = Invoke-StudioApi -Method GET -Path "/api/projects/$ProjectId/commerce/script-units/$ScriptUnitId/storyboard-plans?filter%5Bstatus%5D=active"
$planSummary = @($planList.items | Where-Object active) | Select-Object -First 1
if ($null -eq $planSummary) {
  $planSummary = @($planList.items) | Select-Object -First 1
}
if ($null -eq $planSummary) {
  throw "Commerce ScriptUnit $ScriptUnitId has no active storyboard plan."
}

$planDetail = Invoke-StudioApi -Method GET -Path "/api/projects/$ProjectId/commerce/script-units/$ScriptUnitId/storyboard-plans/$($planSummary.id)"
$availableShots = @($planDetail.shots)
if ($ShotIds.Count -gt 0) {
  $selectedShotIds = @($ShotIds | ForEach-Object { $_.Trim() } | Where-Object { $_ })
  $knownShotIds = @($availableShots | ForEach-Object id)
  $unknownShotIds = @($selectedShotIds | Where-Object { $_ -notin $knownShotIds })
  if ($unknownShotIds.Count -gt 0) {
    throw "Unknown storyboard shot IDs: $($unknownShotIds -join ', ')"
  }
} else {
  $selectedShotIds = @($availableShots | Select-Object -First $ShotCount | ForEach-Object id)
}
if ($selectedShotIds.Count -lt $ShotCount) {
  throw "Storyboard plan has only $($selectedShotIds.Count) selected shots; $ShotCount are required."
}

$targetLanguage = [string]$planDetail.plan.targetLanguage
if ($RequireNonChineseTargetLanguage -and $targetLanguage -match '^zh(?:-|$)') {
  throw "Target language $targetLanguage is Chinese; a non-Chinese ScriptUnit is required."
}

if ($PreflightOnly) {
  $preflightPath = Write-SmokeEvidence -PathPrefix 'commerce-real-provider-preflight' -Payload ([pscustomobject]@{
    startedAt = $startedAt.ToString('O')
    completedAt = [DateTimeOffset]::UtcNow.ToString('O')
    providerSpendConfirmed = $false
    apiBaseUrl = $ApiBaseUrl
    organizationId = $OrganizationId
    projectId = $ProjectId
    workspaceId = [string]$project.workspaceId
    scriptUnitId = $ScriptUnitId
    storyboardPlanId = [string]$planDetail.plan.id
    storyboardPlanRevision = [int]$planDetail.plan.revision
    unitGenerationId = [string]$unit.activeUnitGenerationId
    targetLanguage = $targetLanguage
    shotIds = $selectedShotIds
    workflowTemplateVersionId = [string]$projectOptions.workflowTemplateVersionId
    workflowTemplateVersion = [int]$projectOptions.workflowTemplateVersion
    videoProductionProfileKey = [string]$projectOptions.videoProductionProfileKey
    videoProductionProfileVersion = [int]$projectOptions.videoProductionProfileVersion
    modelRequirements = @($projectOptions.modelRequirements)
  })
  Write-Host "Commerce real-provider preflight passed without Provider calls. Evidence: $preflightPath"
  return
}

$batchBody = @{
  planId = [string]$planDetail.plan.id
  expectedPlanRevision = [int]$planDetail.plan.revision
  expectedUnitGenerationId = [string]$unit.activeUnitGenerationId
  shotIds = $selectedShotIds
  force = [bool]$Force
  concurrency = $Concurrency
}
$requestedStages = switch ($Stage) {
  'full' { @('reference-prompts', 'reference-images', 'video-prompts', 'shot-videos') }
  default { @($Stage) }
}

foreach ($requestedStage in $requestedStages) {
  $idempotencyKey = "commerce-real-smoke-$requestedStage-$([Guid]::NewGuid())"
  switch ($requestedStage) {
    'reference-prompts' {
      $run = Invoke-StudioApi -Method POST `
        -Path "/api/projects/$ProjectId/commerce/script-units/$ScriptUnitId/reference-images/generate-batch" `
        -IdempotencyKey $idempotencyKey `
        -Body ($batchBody + @{ operation = 'generate_prompts' })
      $detail = Invoke-CommerceRunWithRetry -Run $run -Label 'Commerce reference prompt generation'
    }
    'reference-images' {
      $run = Invoke-StudioApi -Method POST `
        -Path "/api/projects/$ProjectId/commerce/script-units/$ScriptUnitId/reference-images/generate-batch" `
        -IdempotencyKey $idempotencyKey `
        -Body ($batchBody + @{ operation = 'generate_images' })
      $detail = Invoke-CommerceRunWithRetry -Run $run -Label 'Commerce real image generation'
      $missingArtifacts = @($detail.items | Where-Object { -not $_.outputArtifactId -or -not $_.outputMediaFileId })
      if ($missingArtifacts.Count -gt 0) {
        throw 'Reference image smoke succeeded without complete artifact/media provenance.'
      }
    }
    'video-prompts' {
      $run = Invoke-StudioApi -Method POST `
        -Path "/api/projects/$ProjectId/commerce/script-units/$ScriptUnitId/video-prompts/generate-batch" `
        -IdempotencyKey $idempotencyKey `
        -Body $batchBody
      $detail = Invoke-CommerceRunWithRetry -Run $run -Label 'Commerce video prompt generation and review'
      $missingPlans = @($detail.items | Where-Object { -not $_.outputVideoPromptPlanId })
      if ($missingPlans.Count -gt 0) {
        throw 'Video prompt smoke succeeded without approved prompt-plan provenance.'
      }
    }
    'shot-videos' {
      $run = Invoke-StudioApi -Method POST `
        -Path "/api/projects/$ProjectId/commerce/script-units/$ScriptUnitId/shot-videos/generate-batch" `
        -IdempotencyKey $idempotencyKey `
        -Body $batchBody
      $detail = Invoke-CommerceRunWithRetry -Run $run -Label 'Commerce real shot-video generation'
      $missingVideoOutputs = @($detail.items | Where-Object { -not $_.outputVideoRenderPlanId -or -not $_.outputArtifactId -or -not $_.outputMediaFileId })
      if ($missingVideoOutputs.Count -gt 0) {
        throw 'Shot-video smoke succeeded without complete Render Plan/artifact/media provenance.'
      }
    }
  }
  Add-RunEvidence -StageName $requestedStage -Detail $detail
}

$evidenceFullPath = Write-SmokeEvidence -PathPrefix 'commerce-real-provider-smoke' -Payload ([pscustomobject]@{
  startedAt = $startedAt.ToString('O')
  completedAt = [DateTimeOffset]::UtcNow.ToString('O')
  providerSpendConfirmed = $true
  apiBaseUrl = $ApiBaseUrl
  organizationId = $OrganizationId
  projectId = $ProjectId
  scriptUnitId = $ScriptUnitId
  storyboardPlanId = [string]$planDetail.plan.id
  storyboardPlanRevision = [int]$planDetail.plan.revision
  unitGenerationId = [string]$unit.activeUnitGenerationId
  targetLanguage = $targetLanguage
  shotIds = $selectedShotIds
  stages = $evidence
})

Write-Host "Commerce real-provider smoke passed. Evidence: $evidenceFullPath"
