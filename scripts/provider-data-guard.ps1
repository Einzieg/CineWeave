[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('Snapshot', 'Verify', 'DrainCheck')]
    [string]$Mode,

    [string]$SnapshotPath = 'tmp/provider-protection-snapshot.json',
    [string]$ComposeFile = 'compose.yml',
    [string]$DatabaseUser = 'cineweave',
    [string]$DatabaseName = 'cineweave'
)

$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true
Set-StrictMode -Version Latest

$configurationTables = @(
    'provider_accounts',
    'provider_connectors',
    'provider_credentials',
    'provider_endpoints',
    'provider_models',
    'provider_model_capabilities',
    'provider_limit_policies',
    'model_profiles',
    'model_profile_bindings'
)
$historyTables = @(
    'provider_call_logs',
    'cost_records',
    'provider_async_tasks'
)

function Invoke-Docker {
    param([Parameter(Mandatory = $true)][string[]]$Arguments)

    $output = @(& docker @Arguments)
    if ($LASTEXITCODE -ne 0) {
        throw "docker command failed with exit code $LASTEXITCODE"
    }
    return $output
}

function Invoke-PsqlText {
    param([Parameter(Mandatory = $true)][string]$Sql)

    $arguments = @(
        'compose', '-f', $ComposeFile, 'exec', '-T', 'postgres',
        'psql', '-U', $DatabaseUser, '-d', $DatabaseName,
        '-v', 'ON_ERROR_STOP=1', '-A', '-t', '-c', $Sql
    )
    $output = Invoke-Docker -Arguments $arguments
    return (($output | ForEach-Object { [string]$_ }) -join "`n").Trim()
}

function Invoke-PsqlJson {
    param([Parameter(Mandatory = $true)][string]$Sql)

    $text = Invoke-PsqlText -Sql $Sql
    if ([string]::IsNullOrWhiteSpace($text)) {
        throw 'database query returned an empty JSON result'
    }
    return $text | ConvertFrom-Json -Depth 32
}

function Assert-ProviderConfigurationFrozen {
    $containerIds = Invoke-Docker -Arguments @('compose', '-f', $ComposeFile, 'ps', '-q', 'api')
    $containerId = (($containerIds | ForEach-Object { [string]$_ }) -join '').Trim()
    if ([string]::IsNullOrWhiteSpace($containerId)) {
        return
    }

    $environment = Invoke-Docker -Arguments @(
        'inspect', '--format', '{{range .Config.Env}}{{println .}}{{end}}', $containerId
    )
    $frozen = $environment | Where-Object {
        [string]$_ -match '^CINEWEAVE_PROVIDER_CONFIGURATION_FROZEN=(?i:true|1)$'
    }
    if (-not $frozen) {
        throw 'API is running without CINEWEAVE_PROVIDER_CONFIGURATION_FROZEN=true. Freeze Provider configuration writes or stop API before snapshot/verification.'
    }
}

function Get-ConfigurationFingerprint {
    param([Parameter(Mandatory = $true)][string]$Table)

    $sql = @"
WITH guarded_rows AS (
    SELECT
        id::text AS id,
        encode(public.digest(pg_catalog.convert_to(to_jsonb(source_row)::text, 'UTF8'), 'sha256'), 'hex') AS row_hash
    FROM public.$Table AS source_row
)
SELECT json_build_object(
    'rowCount', count(*),
    'hash', encode(
        public.digest(
            pg_catalog.convert_to(COALESCE(string_agg(row_hash, '' ORDER BY id), ''), 'UTF8'),
            'sha256'
        ),
        'hex'
    ),
    'ids', COALESCE(json_agg(id ORDER BY id), '[]'::json)
)::text
FROM guarded_rows;
"@
    return Invoke-PsqlJson -Sql $sql
}

function Get-HistoryIdentitySet {
    param([Parameter(Mandatory = $true)][string]$Table)

    $sql = @"
SELECT json_build_object(
    'rowCount', count(*),
    'ids', COALESCE(json_agg(id::text ORDER BY id::text), '[]'::json)
)::text
FROM public.$Table;
"@
    return Invoke-PsqlJson -Sql $sql
}

function Get-Snapshot {
    $configuration = [ordered]@{}
    foreach ($table in $configurationTables) {
        $configuration[$table] = Get-ConfigurationFingerprint -Table $table
    }

    $history = [ordered]@{}
    foreach ($table in $historyTables) {
        $history[$table] = Get-HistoryIdentitySet -Table $table
    }

    return [ordered]@{
        schemaVersion = 1
        capturedAt = [DateTimeOffset]::UtcNow.ToString('O')
        database = $DatabaseName
        configuration = $configuration
        history = $history
    }
}

function Assert-ExactIdentitySet {
    param(
        [Parameter(Mandatory = $true)][string]$Table,
        [Parameter(Mandatory = $true)]$Expected,
        [Parameter(Mandatory = $true)]$Actual
    )

    $difference = @(Compare-Object -ReferenceObject @($Expected.ids) -DifferenceObject @($Actual.ids))
    if ($difference.Count -ne 0) {
        throw "protected Provider table $Table primary-key set changed"
    }
    if ([long]$Expected.rowCount -ne [long]$Actual.rowCount) {
        throw "protected Provider table $Table row count changed from $($Expected.rowCount) to $($Actual.rowCount)"
    }
    if ([string]$Expected.hash -cne [string]$Actual.hash) {
        throw "protected Provider table $Table configuration hash changed"
    }
}

function Assert-HistorySubset {
    param(
        [Parameter(Mandatory = $true)][string]$Table,
        [Parameter(Mandatory = $true)]$Expected,
        [Parameter(Mandatory = $true)]$Actual
    )

    $actualIds = [Collections.Generic.HashSet[string]]::new([StringComparer]::Ordinal)
    foreach ($id in @($Actual.ids)) {
        [void]$actualIds.Add([string]$id)
    }
    $missing = @($Expected.ids | Where-Object { -not $actualIds.Contains([string]$_) })
    if ($missing.Count -ne 0) {
        $sample = ($missing | Select-Object -First 5) -join ', '
        throw "Provider history table $Table lost $($missing.Count) baseline rows; sample: $sample"
    }
}

function Test-DrainState {
    $workflowTypes = @(
        'video_production',
        'batch_generate_shot_videos',
        'script_to_video',
        'full_production',
        'regenerate_shot_video'
    ) | ForEach-Object { "'$_'" }
    $workflowTypeSql = $workflowTypes -join ', '

    $state = Invoke-PsqlJson -Sql @"
SELECT json_build_object(
    'activeWorkflowRuns', COALESCE((
        SELECT json_agg(json_build_object(
            'id', id::text,
            'workflowType', workflow_type,
            'status', status,
            'createdAt', created_at
        ) ORDER BY created_at)
        FROM workflow_runs
        WHERE workflow_type IN ($workflowTypeSql)
          AND status IN ('pending', 'queued', 'running', 'cancelling', 'waiting_review')
    ), '[]'::json),
    'activeVideoTasks', COALESCE((
        SELECT json_agg(json_build_object(
            'id', id::text,
            'taskType', task_type,
            'status', status,
            'createdAt', created_at
        ) ORDER BY created_at)
        FROM provider_async_tasks
        WHERE task_type LIKE 'video.%'
          AND status IN ('queued', 'running', 'cancelling')
    ), '[]'::json),
    'activeVideoLeases', COALESCE((
        SELECT json_agg(json_build_object(
            'id', id::text,
            'taskType', task_type,
            'status', status,
            'expiresAt', expires_at
        ) ORDER BY created_at)
        FROM provider_leases
        WHERE task_type LIKE 'video.%'
          AND status = 'active'
          AND expires_at > now()
    ), '[]'::json)
)::text;
"@

    $activeWorkflowRuns = @($state.activeWorkflowRuns)
    $activeVideoTasks = @($state.activeVideoTasks)
    $activeVideoLeases = @($state.activeVideoLeases)
    if ($activeWorkflowRuns.Count -ne 0 -or $activeVideoTasks.Count -ne 0 -or $activeVideoLeases.Count -ne 0) {
        $details = $state | ConvertTo-Json -Depth 8 -Compress
        throw "video production runtime is not drained: $details"
    }

    [pscustomobject]@{
        status = 'drained'
        activeWorkflowRuns = 0
        activeVideoTasks = 0
        activeVideoLeases = 0
    } | ConvertTo-Json -Compress
}

switch ($Mode) {
    'Snapshot' {
        Assert-ProviderConfigurationFrozen
        $snapshot = Get-Snapshot
        $resolvedPath = [IO.Path]::GetFullPath((Join-Path (Get-Location) $SnapshotPath))
        $directory = Split-Path -Parent $resolvedPath
        if (-not (Test-Path -LiteralPath $directory)) {
            New-Item -ItemType Directory -Path $directory | Out-Null
        }
        $snapshot | ConvertTo-Json -Depth 12 | Set-Content -LiteralPath $resolvedPath -Encoding UTF8
        [pscustomobject]@{
            status = 'snapshot_created'
            path = $resolvedPath
            configurationTables = $configurationTables.Count
            historyTables = $historyTables.Count
        } | ConvertTo-Json -Compress
    }
    'Verify' {
        Assert-ProviderConfigurationFrozen
        if (-not (Test-Path -LiteralPath $SnapshotPath)) {
            throw "Provider protection snapshot was not found: $SnapshotPath"
        }
        $expected = Get-Content -LiteralPath $SnapshotPath -Encoding UTF8 -Raw | ConvertFrom-Json -Depth 32
        if ([int]$expected.schemaVersion -ne 1) {
            throw "unsupported Provider protection snapshot version: $($expected.schemaVersion)"
        }
        $actual = Get-Snapshot
        foreach ($table in $configurationTables) {
            Assert-ExactIdentitySet -Table $table -Expected $expected.configuration.$table -Actual $actual.configuration.$table
        }
        foreach ($table in $historyTables) {
            Assert-HistorySubset -Table $table -Expected $expected.history.$table -Actual $actual.history.$table
        }
        [pscustomobject]@{
            status = 'verified'
            configurationTables = $configurationTables.Count
            historyTables = $historyTables.Count
        } | ConvertTo-Json -Compress
    }
    'DrainCheck' {
        Test-DrainState
    }
}
