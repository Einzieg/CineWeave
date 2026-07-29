param([switch]$KeepStack)

$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true
Set-StrictMode -Version Latest

$repositoryRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
$suffix = [Guid]::NewGuid().ToString('N').Substring(0, 12)
$projectName = "cineweave-ce-fresh-$suffix"
$networkName = "${projectName}_internal"
$overridePath = Join-Path ([IO.Path]::GetTempPath()) "$projectName.override.yml"
$composeFiles = @('-f', (Join-Path $repositoryRoot 'compose.yml'), '-f', $overridePath)
$savedEnvironment = @{}
$testEnvironmentNames = [System.Collections.Generic.HashSet[string]]::new(
  [StringComparer]::OrdinalIgnoreCase
)
$reservedPorts = [System.Collections.Generic.HashSet[int]]::new()
$started = $false

function Get-UniqueFreeTcpPort {
  for ($attempt = 0; $attempt -lt 32; $attempt++) {
    $listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 0)
    try {
      $listener.Start()
      $port = ([Net.IPEndPoint]$listener.LocalEndpoint).Port
    } finally {
      $listener.Stop()
    }
    if ($reservedPorts.Add($port)) {
      return $port
    }
  }
  throw 'Could not reserve a unique local TCP port for the CE fresh-install test.'
}

function Save-EnvironmentValue {
  param([Parameter(Mandatory = $true)][string]$Name)
  if ($savedEnvironment.ContainsKey($Name)) {
    return
  }
  $savedEnvironment[$Name] = [Environment]::GetEnvironmentVariable($Name, 'Process')
}

function Set-TestEnvironmentValue {
  param(
    [Parameter(Mandatory = $true)][string]$Name,
    [Parameter(Mandatory = $true)][AllowEmptyString()][string]$Value
  )
  Save-EnvironmentValue -Name $Name
  $testEnvironmentNames.Add($Name) | Out-Null
  [Environment]::SetEnvironmentVariable($Name, $Value, 'Process')
}

function Remove-TestEnvironmentValue {
  param([Parameter(Mandatory = $true)][string]$Name)
  Save-EnvironmentValue -Name $Name
  $testEnvironmentNames.Add($Name) | Out-Null
  [Environment]::SetEnvironmentVariable($Name, $null, 'Process')
}

function Restore-Environment {
  foreach ($name in $testEnvironmentNames) {
    [Environment]::SetEnvironmentVariable(
      $name,
      $savedEnvironment[$name],
      'Process'
    )
  }
}

function Invoke-Compose {
  param(
    [Parameter(Mandatory = $true)]
    [string[]]$Arguments
  )
  & docker compose -p $projectName @composeFiles @Arguments
  if ($LASTEXITCODE -ne 0) {
    throw "docker compose failed with exit code $LASTEXITCODE`: $($Arguments -join ' ')"
  }
}

function Assert-HttpStatus {
  param(
    [Parameter(Mandatory = $true)][string]$Uri,
    [Parameter(Mandatory = $true)][int]$ExpectedStatus
  )
  try {
    $response = Invoke-WebRequest -Uri $Uri -UseBasicParsing -TimeoutSec 15 -SkipHttpErrorCheck
  } catch {
    throw "HTTP request failed for ${Uri}: $($_.Exception.Message)"
  }
  if ([int]$response.StatusCode -ne $ExpectedStatus) {
    throw "HTTP $Uri returned $($response.StatusCode), expected $ExpectedStatus."
  }
}

$apiPort = Get-UniqueFreeTcpPort
$realtimePort = Get-UniqueFreeTcpPort
$webPort = Get-UniqueFreeTcpPort
$minioPort = Get-UniqueFreeTcpPort
$postgresPort = Get-UniqueFreeTcpPort

$override = @"
services:
  postgres:
    ports:
      - "127.0.0.1:${postgresPort}:5432"
"@
$override | Set-Content -LiteralPath $overridePath -Encoding UTF8

Push-Location $repositoryRoot
try {
  foreach ($entry in Get-ChildItem Env:) {
    if ($entry.Name -match '^(CINEWEAVE_(LICENSE|COMMERCIAL|NEW_API|BILLING)|NEW_API_)') {
      Remove-TestEnvironmentValue -Name $entry.Name
    }
  }

  Set-TestEnvironmentValue -Name 'CINEWEAVE_ENV' -Value 'development'
  Set-TestEnvironmentValue -Name 'CINEWEAVE_EDITION' -Value 'community'
  Set-TestEnvironmentValue -Name 'CINEWEAVE_WEB_EDITION' -Value 'community'
  Set-TestEnvironmentValue -Name 'CINEWEAVE_RELEASE_ID' -Value "ce-fresh-$suffix"
  Set-TestEnvironmentValue -Name 'CINEWEAVE_INTERNAL_NETWORK' -Value $networkName
  Set-TestEnvironmentValue -Name 'CINEWEAVE_API_HOST_PORT' -Value ([string]$apiPort)
  Set-TestEnvironmentValue -Name 'CINEWEAVE_REALTIME_HOST_PORT' -Value ([string]$realtimePort)
  Set-TestEnvironmentValue -Name 'CINEWEAVE_WEB_HOST_PORT' -Value ([string]$webPort)
  Set-TestEnvironmentValue -Name 'MINIO_API_HOST_PORT' -Value ([string]$minioPort)
  Set-TestEnvironmentValue -Name 'NEXT_PUBLIC_API_BASE_URL' -Value "http://127.0.0.1:$apiPort"
  Set-TestEnvironmentValue -Name 'NEXT_PUBLIC_REALTIME_URL' -Value "http://127.0.0.1:$realtimePort/api/realtime/events"
  Set-TestEnvironmentValue -Name 'S3_PUBLIC_ENDPOINT' -Value "http://127.0.0.1:$minioPort"
  Set-TestEnvironmentValue -Name 'CINEWEAVE_CORS_ORIGINS' -Value "http://127.0.0.1:$webPort"
  Set-TestEnvironmentValue -Name 'CINEWEAVE_SERVICE_TOKEN' -Value "ce-fresh-service-$suffix"
  Set-TestEnvironmentValue -Name 'TEMPORAL_WORKER_VERSIONING_ENABLED' -Value 'false'

  Invoke-Compose -Arguments @('--profile', 'app', 'config', '--quiet')

  Invoke-Compose -Arguments @(
    '--profile',
    'app',
    'build',
    '--quiet'
  )

  $upArguments = @(
    '--profile',
    'app',
    'up',
    '-d',
    '--no-build',
    '--wait',
    '--wait-timeout',
    '900'
  )
  $started = $true
  Invoke-Compose -Arguments $upArguments

  $rows = @(
    Invoke-Compose -Arguments @(
      '--profile',
      'app',
      'ps',
      '--all',
      '--format',
      '{{.Service}}|{{.State}}|{{.Health}}'
    ) |
      ForEach-Object {
        if (-not [string]::IsNullOrWhiteSpace($_)) {
          $fields = @([string]$_ -split '\|', 3)
          if ($fields.Count -ne 3) {
            throw "Unexpected docker compose ps row: $_"
          }
          [pscustomobject]@{
            Service = $fields[0]
            State = $fields[1]
            Health = $fields[2]
          }
        }
      }
  )
  $requiredRunning = @(
    'postgres',
    'redis',
    'minio',
    'nats',
    'temporal',
    'api',
    'realtime',
    'script-worker',
    'agent-worker',
    'media-worker',
    'audio-worker',
    'provider-gateway',
    'event-publisher',
    'web'
  )
  foreach ($service in $requiredRunning) {
    $row = @($rows | Where-Object Service -eq $service)
    if ($row.Count -ne 1 -or $row[0].State -ne 'running') {
      throw "CE fresh-install service '$service' is not running exactly once."
    }
    if (
      -not [string]::IsNullOrWhiteSpace([string]$row[0].Health) -and
      [string]$row[0].Health -ne 'healthy'
    ) {
      throw "CE fresh-install service '$service' health is '$($row[0].Health)'."
    }
  }

  $serviceNames = @($rows | ForEach-Object { [string]$_.Service })
  $forbiddenServices = @(
    $serviceNames |
      Where-Object { $_ -match '(commercial|billing-bridge|license-issuer)' }
  )
  if ($forbiddenServices.Count -gt 0) {
    throw "CE fresh install contains commercial services: $($forbiddenServices -join ', ')"
  }

  $containerIds = @(
    & docker ps --filter "label=com.docker.compose.project=$projectName" --format '{{.ID}}'
  )
  foreach ($containerId in $containerIds) {
    $environmentNames = @(
      & docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' $containerId |
        ForEach-Object { ([string]$_ -split '=', 2)[0] }
    )
    $forbiddenEnvironment = @(
      $environmentNames |
        Where-Object {
          $_ -match '^(CINEWEAVE_(LICENSE|COMMERCIAL|NEW_API|BILLING)|NEW_API_)'
        }
    )
    if ($forbiddenEnvironment.Count -gt 0) {
      throw "CE container $containerId contains commercial environment names: $($forbiddenEnvironment -join ', ')"
    }
  }

  Assert-HttpStatus -Uri "http://127.0.0.1:$apiPort/healthz" -ExpectedStatus 200
  Assert-HttpStatus -Uri "http://127.0.0.1:$apiPort/readyz" -ExpectedStatus 200
  Assert-HttpStatus -Uri "http://127.0.0.1:$realtimePort/healthz" -ExpectedStatus 200
  Assert-HttpStatus -Uri "http://127.0.0.1:$webPort/" -ExpectedStatus 200
  Assert-HttpStatus -Uri "http://127.0.0.1:$apiPort/api/billing/accounts" -ExpectedStatus 404

  $edition = Invoke-RestMethod -Uri "http://127.0.0.1:$apiPort/api/system/edition" -TimeoutSec 15
  $commercialReleaseProperty = $edition.data.PSObject.Properties['commercialReleaseId']
  $compiledModules = $edition.data.compiledModules
  if (
    [string]$edition.data.deploymentEdition -ne 'community' -or
    [string]$edition.data.coreReleaseId -ne "ce-fresh-$suffix" -or
    (
      $null -ne $commercialReleaseProperty -and
      $null -ne $commercialReleaseProperty.Value
    ) -or
    ($null -ne $compiledModules -and @($compiledModules).Count -ne 0)
  ) {
    throw "CE runtime Edition identity is invalid: $($edition.data | ConvertTo-Json -Depth 6 -Compress)"
  }

  $previousIntegration = $env:CINEWEAVE_INTEGRATION_TEST
  $previousDatabaseURL = $env:DATABASE_URL
  try {
    $env:CINEWEAVE_INTEGRATION_TEST = '1'
    $env:DATABASE_URL = "postgres://cineweave:cineweave_dev_password@127.0.0.1:$postgresPort/cineweave?sslmode=disable"
    $previousNativeErrorPreference = $PSNativeCommandUseErrorActionPreference
    try {
      $PSNativeCommandUseErrorActionPreference = $false
      $goOutput = @(
        & go test -tags community ./internal/workflows -run '^TestWorkflowGatewayIntegration$' -count=1 2>&1
      )
      $goExitCode = $LASTEXITCODE
    } finally {
      $PSNativeCommandUseErrorActionPreference = $previousNativeErrorPreference
    }
    $goOutput | ForEach-Object { Write-Host $_ }
    if ($goExitCode -ne 0) {
      throw "CE zero-cost text production integration failed with exit code $goExitCode."
    }
  } finally {
    $env:CINEWEAVE_INTEGRATION_TEST = $previousIntegration
    $env:DATABASE_URL = $previousDatabaseURL
  }

  Write-Host (
    "CE fresh install passed: project=$projectName release=ce-fresh-$suffix " +
    'commercialServices=0 commercialCredentialNames=0 paidProviderCalls=0 ' +
    'coreTextWorkflow=passed'
  )
} finally {
  Pop-Location
  if ($started -and -not $KeepStack) {
    try {
      & docker compose -p $projectName @composeFiles --profile app down --volumes --remove-orphans
    } catch {
      Write-Warning "CE fresh-install cleanup failed: $($_.Exception.Message)"
    }
  } elseif ($KeepStack) {
    Write-Warning "CE fresh-install stack kept by request: $projectName; override=$overridePath"
  }
  if (-not $KeepStack -and (Test-Path -LiteralPath $overridePath)) {
    Remove-Item -LiteralPath $overridePath -Force
  }
  Restore-Environment
}
