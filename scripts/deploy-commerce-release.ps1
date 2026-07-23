[CmdletBinding()]
param(
  [ValidateSet('Deploy', 'Smoke', 'Full')]
  [string]$Phase = 'Deploy',
  [switch]$ConfirmMainEnvironmentMigration,
  [switch]$SkipReleaseCheck,
  [switch]$RunPaidSmoke,
  [switch]$ConfirmProviderSpend,
  [string]$ComposeFile = 'compose.yml',
  [string]$SnapshotPath = 'tmp/provider-protection-before-commerce-release.json',
  [string]$ApiBaseUrl = 'http://localhost:19288',
  [string]$WebBaseUrl = 'http://localhost:19285',
  [ValidateRange(30, 1800)]
  [int]$HealthTimeoutSeconds = 300,
  [ValidateRange(1, 20)]
  [int]$ShotCount = 3,
  [ValidateRange(30, 14400)]
  [int]$SmokeTimeoutSeconds = 3600
)

$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true
Set-StrictMode -Version Latest

$runDeploy = $Phase -in @('Deploy', 'Full')
$runSmoke = $Phase -in @('Smoke', 'Full')

if ($runDeploy -and -not $ConfirmMainEnvironmentMigration) {
  throw 'Main-environment migration is disabled by default. Re-run with -ConfirmMainEnvironmentMigration after an explicit deployment approval.'
}
if ($RunPaidSmoke -and -not $runSmoke) {
  throw 'Paid Commerce smoke requires -Phase Smoke or -Phase Full.'
}
if ($RunPaidSmoke -and -not $ConfirmProviderSpend) {
  throw 'Paid Commerce smoke is disabled by default. Re-run with -RunPaidSmoke -ConfirmProviderSpend after explicit spend approval.'
}

$utf8NoBom = [System.Text.UTF8Encoding]::new($false)
[Console]::OutputEncoding = $utf8NoBom
$OutputEncoding = $utf8NoBom

$repoRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
$resolvedComposeFile = (Resolve-Path -LiteralPath (Join-Path $repoRoot $ComposeFile)).Path
$providerGuardScript = Join-Path $repoRoot 'scripts/provider-data-guard.ps1'
$releaseCheckScript = Join-Path $repoRoot 'scripts/release-check.ps1'
$smokeScript = Join-Path $repoRoot 'scripts/smoke-commerce-real-provider.ps1'
$requiredSmokeVariables = @(
  'CINEWEAVE_SMOKE_ACCESS_TOKEN',
  'CINEWEAVE_SMOKE_ORGANIZATION_ID',
  'CINEWEAVE_SMOKE_PROJECT_ID',
  'CINEWEAVE_SMOKE_SCRIPT_UNIT_ID'
)
$requiredRuntimeServices = @(
  'postgres',
  'redis',
  'minio',
  'nats',
  'temporal',
  'provider-gateway',
  'script-worker',
  'agent-worker',
  'media-worker',
  'audio-worker',
  'event-publisher',
  'api',
  'realtime',
  'web'
)

function Invoke-NativeCommand {
  param(
    [Parameter(Mandatory = $true)]
    [string]$FilePath,
    [Parameter(Mandatory = $true)]
    [string[]]$ArgumentList,
    [switch]$CaptureOutput
  )

  if ($CaptureOutput) {
    $output = @(& $FilePath @ArgumentList)
  } else {
    & $FilePath @ArgumentList
    $output = @()
  }
  if ($LASTEXITCODE -ne 0) {
    throw "$FilePath failed with exit code $LASTEXITCODE"
  }
  return $output
}

function Invoke-Compose {
  param(
    [Parameter(Mandatory = $true)]
    [string[]]$Arguments,
    [switch]$CaptureOutput
  )

  $composeArguments = @('compose', '-f', $resolvedComposeFile) + $Arguments
  return Invoke-NativeCommand -FilePath 'docker' -ArgumentList $composeArguments -CaptureOutput:$CaptureOutput
}

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

function Assert-SmokeConfiguration {
  $missing = @()
  foreach ($name in $requiredSmokeVariables) {
    $value = [Environment]::GetEnvironmentVariable($name, 'Process')
    if ([string]::IsNullOrWhiteSpace($value)) {
      $missing += $name
    }
  }
  if ($missing.Count -ne 0) {
    throw "Commerce smoke configuration is incomplete: $($missing -join ', ')"
  }
}

function Get-ApiContainerID {
  $containerID = (
    Invoke-Compose -Arguments @('ps', '-q', 'api') -CaptureOutput |
      ForEach-Object { [string]$_ }
  ) -join ''
  return $containerID.Trim()
}

function Get-ApiProviderConfigurationFrozen {
  $containerID = Get-ApiContainerID
  if ([string]::IsNullOrWhiteSpace($containerID)) {
    throw 'The main API container is not running; refusing to infer its Provider configuration state.'
  }
  $environment = Invoke-NativeCommand -FilePath 'docker' -ArgumentList @(
    'inspect',
    '--format',
    '{{range .Config.Env}}{{println .}}{{end}}',
    $containerID
  ) -CaptureOutput
  return [bool]($environment | Where-Object {
    [string]$_ -match '^CINEWEAVE_PROVIDER_CONFIGURATION_FROZEN=(?i:true|1)$'
  })
}

function Wait-ComposeServiceHealthy {
  param(
    [Parameter(Mandatory = $true)]
    [string]$Service
  )

  $deadline = [DateTimeOffset]::UtcNow.AddSeconds($HealthTimeoutSeconds)
  do {
    $containerID = (
      Invoke-Compose -Arguments @('ps', '-q', $Service) -CaptureOutput |
        ForEach-Object { [string]$_ }
    ) -join ''
    $containerID = $containerID.Trim()
    if (-not [string]::IsNullOrWhiteSpace($containerID)) {
      $state = (
        Invoke-NativeCommand -FilePath 'docker' -ArgumentList @(
          'inspect',
          '--format',
          '{{.State.Status}}|{{if .State.Health}}{{.State.Health.Status}}{{end}}',
          $containerID
        ) -CaptureOutput |
          ForEach-Object { [string]$_ }
      ) -join ''
      $parts = $state.Trim().Split('|', 2)
      $runtimeState = $parts[0]
      $healthState = if ($parts.Count -gt 1) { $parts[1] } else { '' }
      if ($runtimeState -eq 'running' -and ($healthState -eq '' -or $healthState -eq 'healthy')) {
        return
      }
    }
    Start-Sleep -Seconds 2
  } while ([DateTimeOffset]::UtcNow -lt $deadline)

  throw "Compose service $Service did not become healthy within $HealthTimeoutSeconds seconds."
}

function Set-ApiProviderConfigurationFrozen {
  param([Parameter(Mandatory = $true)][bool]$Frozen)

  $env:CINEWEAVE_PROVIDER_CONFIGURATION_FROZEN = if ($Frozen) { 'true' } else { 'false' }
  Invoke-Compose -Arguments @(
    '--profile',
    'app',
    'up',
    '-d',
    '--no-deps',
    '--force-recreate',
    'api'
  )
  Wait-ComposeServiceHealthy -Service 'api'
  $actual = Get-ApiProviderConfigurationFrozen
  if ($actual -ne $Frozen) {
    throw "API Provider configuration freeze state is $actual; expected $Frozen."
  }
}

function Wait-HttpEndpoint {
  param(
    [Parameter(Mandatory = $true)]
    [string]$Url,
    [Parameter(Mandatory = $true)]
    [string]$Name
  )

  $deadline = [DateTimeOffset]::UtcNow.AddSeconds($HealthTimeoutSeconds)
  do {
    try {
      $response = Invoke-WebRequest -Uri $Url -Method GET -TimeoutSec 10 -UseBasicParsing
      if ($response.StatusCode -ge 200 -and $response.StatusCode -lt 400) {
        return
      }
    } catch {
      if ([DateTimeOffset]::UtcNow -ge $deadline) {
        throw
      }
    }
    Start-Sleep -Seconds 2
  } while ([DateTimeOffset]::UtcNow -lt $deadline)

  throw "$Name did not become ready within $HealthTimeoutSeconds seconds: $Url"
}

function Get-ExpectedMigrationVersion {
  $versions = @(
    Get-ChildItem -LiteralPath (Join-Path $repoRoot 'db/migrations') -Filter '*.sql' |
      ForEach-Object {
        if ($_.Name -match '^(?<version>\d{6})_') {
          [int]$Matches.version
        }
      }
  )
  if ($versions.Count -eq 0) {
    throw 'No numbered database migrations were found.'
  }
  return ($versions | Measure-Object -Maximum).Maximum
}

function Get-AppliedMigrationVersion {
  $sql = "SELECT COALESCE(max(version_id), 0) FROM cineweave_migrations.cineweave_schema_versions WHERE is_applied;"
  $output = Invoke-Compose -Arguments @(
    'exec',
    '-T',
    'postgres',
    'psql',
    '-U',
    'cineweave',
    '-d',
    'cineweave',
    '-v',
    'ON_ERROR_STOP=1',
    '-A',
    '-t',
    '-c',
    $sql
  ) -CaptureOutput
  $raw = (($output | ForEach-Object { [string]$_ }) -join '').Trim()
  $version = 0
  if (-not [int]::TryParse($raw, [ref]$version)) {
    throw "Cannot parse the applied database migration version: $raw"
  }
  return $version
}

function Get-RequiredRuntimeServiceProblems {
  $rows = @(
    Invoke-Compose -Arguments @('--profile', 'app', 'ps', '--format', 'json') -CaptureOutput |
      Where-Object { -not [string]::IsNullOrWhiteSpace([string]$_) } |
      ForEach-Object { [string]$_ | ConvertFrom-Json }
  )
  $byService = @{}
  foreach ($row in $rows) {
    $byService[[string]$row.Service] = $row
  }
  $problems = @()
  foreach ($service in $requiredRuntimeServices) {
    if (-not $byService.ContainsKey($service)) {
      $problems += "${service}: missing"
      continue
    }
    $row = $byService[$service]
    if ([string]$row.State -ne 'running') {
      $problems += "${service}: state=$($row.State)"
      continue
    }
    if (-not [string]::IsNullOrWhiteSpace([string]$row.Health) -and [string]$row.Health -ne 'healthy') {
      $problems += "${service}: health=$($row.Health)"
    }
  }
  return @($problems)
}

function Wait-RequiredRuntimeServices {
  $deadline = [DateTimeOffset]::UtcNow.AddSeconds($HealthTimeoutSeconds)
  do {
    $problems = @(Get-RequiredRuntimeServiceProblems)
    if ($problems.Count -eq 0) {
      return
    }
    Start-Sleep -Seconds 2
  } while ([DateTimeOffset]::UtcNow -lt $deadline)

  $problems = @(Get-RequiredRuntimeServiceProblems)
  if ($problems.Count -ne 0) {
    throw "Required Compose services did not become ready within $HealthTimeoutSeconds seconds: $($problems -join '; ')"
  }
}

$hadOriginalFreezeEnvironment = Test-Path Env:CINEWEAVE_PROVIDER_CONFIGURATION_FROZEN
$originalFreezeEnvironment = if ($hadOriginalFreezeEnvironment) {
  $env:CINEWEAVE_PROVIDER_CONFIGURATION_FROZEN
} else {
  $null
}
$originalApiFrozen = $false
$freezeStateRead = $false
$apiFreezeChanged = $false
$migrationStarted = $false
$releaseSucceeded = $false

Push-Location $repoRoot
try {
  if ($runSmoke) {
    Assert-SmokeConfiguration
  }
  $originalApiFrozen = Get-ApiProviderConfigurationFrozen
  $freezeStateRead = $true

  if ($runDeploy -and -not $SkipReleaseCheck) {
    Invoke-ReleaseStep 'Release candidate validation' {
      Invoke-NativeCommand -FilePath 'pwsh' -ArgumentList @(
        '-NoProfile',
        '-File',
        $releaseCheckScript,
        '-CheckProviderDrain',
        '-SkipImageBuild'
      )
    }
  }

  Invoke-ReleaseStep 'Provider runtime drain check' {
    Invoke-NativeCommand -FilePath 'pwsh' -ArgumentList @(
      '-NoProfile',
      '-File',
      $providerGuardScript,
      '-Mode',
      'DrainCheck',
      '-ComposeFile',
      $resolvedComposeFile
    )
  }

  if (-not $originalApiFrozen) {
    Invoke-ReleaseStep 'Freeze Provider configuration writes' {
      Set-ApiProviderConfigurationFrozen -Frozen $true
    }
    $apiFreezeChanged = $true
  } else {
    $env:CINEWEAVE_PROVIDER_CONFIGURATION_FROZEN = 'true'
  }

  Invoke-ReleaseStep 'Recheck drained runtime under Provider freeze' {
    Invoke-NativeCommand -FilePath 'pwsh' -ArgumentList @(
      '-NoProfile',
      '-File',
      $providerGuardScript,
      '-Mode',
      'DrainCheck',
      '-ComposeFile',
      $resolvedComposeFile
    )
  }

  Invoke-ReleaseStep 'Snapshot protected Provider configuration' {
    Invoke-NativeCommand -FilePath 'pwsh' -ArgumentList @(
      '-NoProfile',
      '-File',
      $providerGuardScript,
      '-Mode',
      'Snapshot',
      '-SnapshotPath',
      $SnapshotPath,
      '-ComposeFile',
      $resolvedComposeFile
    )
  }

  if ($runDeploy) {
    $migrationStarted = $true
    Invoke-ReleaseStep 'Build and deploy the app profile' {
      Invoke-Compose -Arguments @('--profile', 'app', 'up', '-d', '--build')
    }

    Invoke-ReleaseStep 'Verify Compose runtime health' {
      Wait-RequiredRuntimeServices
      Wait-HttpEndpoint -Url "$($ApiBaseUrl.TrimEnd('/'))/readyz" -Name 'API'
      Wait-HttpEndpoint -Url $WebBaseUrl -Name 'Web'
    }
  } else {
    Invoke-ReleaseStep 'Verify existing Compose runtime health' {
      Wait-RequiredRuntimeServices
      Wait-HttpEndpoint -Url "$($ApiBaseUrl.TrimEnd('/'))/readyz" -Name 'API'
      Wait-HttpEndpoint -Url $WebBaseUrl -Name 'Web'
    }
  }

  Invoke-ReleaseStep 'Verify database migration head' {
    $expectedVersion = Get-ExpectedMigrationVersion
    $actualVersion = Get-AppliedMigrationVersion
    if ($actualVersion -ne $expectedVersion) {
      throw "Database migration version is $actualVersion; expected $expectedVersion."
    }
    Write-Host "Database migration version: $actualVersion"
  }

  Invoke-ReleaseStep 'Verify protected Provider configuration' {
    Invoke-NativeCommand -FilePath 'pwsh' -ArgumentList @(
      '-NoProfile',
      '-File',
      $providerGuardScript,
      '-Mode',
      'Verify',
      '-SnapshotPath',
      $SnapshotPath,
      '-ComposeFile',
      $resolvedComposeFile
    )
  }

  if ($runSmoke) {
    Invoke-ReleaseStep 'Run zero-cost Commerce Provider preflight' {
      Invoke-NativeCommand -FilePath 'pwsh' -ArgumentList @(
        '-NoProfile',
        '-File',
        $smokeScript,
        '-PreflightOnly',
        '-ShotCount',
        [string]$ShotCount
      )
    }

    if ($RunPaidSmoke) {
      Invoke-ReleaseStep 'Run paid Commerce real-provider smoke' {
        Invoke-NativeCommand -FilePath 'pwsh' -ArgumentList @(
          '-NoProfile',
          '-File',
          $smokeScript,
          '-Stage',
          'full',
          '-ShotCount',
          [string]$ShotCount,
          '-TimeoutSeconds',
          [string]$SmokeTimeoutSeconds,
          '-RetryFailedOnce',
          '-ConfirmProviderSpend'
        )
      }
    }
  }

  Invoke-ReleaseStep 'Final Provider configuration verification' {
    Invoke-NativeCommand -FilePath 'pwsh' -ArgumentList @(
      '-NoProfile',
      '-File',
      $providerGuardScript,
      '-Mode',
      'Verify',
      '-SnapshotPath',
      $SnapshotPath,
      '-ComposeFile',
      $resolvedComposeFile
    )
  }

  $releaseSucceeded = $true
} finally {
  try {
    if ($freezeStateRead -and $apiFreezeChanged) {
      if ($releaseSucceeded -or -not $migrationStarted) {
        Write-Host "`n==> Restore original Provider configuration state"
        Set-ApiProviderConfigurationFrozen -Frozen $originalApiFrozen
      } else {
        Write-Warning 'Deployment did not complete after migration started. Provider configuration writes remain frozen for manual recovery.'
      }
    }
  } finally {
    if ($hadOriginalFreezeEnvironment) {
      $env:CINEWEAVE_PROVIDER_CONFIGURATION_FROZEN = $originalFreezeEnvironment
    } else {
      Remove-Item Env:CINEWEAVE_PROVIDER_CONFIGURATION_FROZEN -ErrorAction SilentlyContinue
    }
    Pop-Location
  }
}

Write-Host "`nCommerce release phase $Phase completed successfully."
