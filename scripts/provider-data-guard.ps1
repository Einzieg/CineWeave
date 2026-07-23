[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('Snapshot', 'Verify', 'DrainCheck', 'Inspect')]
    [string]$Mode,

    [string]$SnapshotPath = 'tmp/provider-protection-snapshot.json',
    [string]$ComposeFile = 'compose.yml',
    [string]$DatabaseUser = 'cineweave',
    [string]$DatabaseName = 'cineweave',
    [ValidateRange(300, 86400)]
    [int]$StandaloneCallActiveWindowSeconds = 1800
)

$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true
Set-StrictMode -Version Latest

$configurationTables = [ordered]@{
    provider_accounts = 'id::text'
    provider_connectors = 'id::text'
    provider_credentials = 'id::text'
    provider_endpoints = 'id::text'
    provider_models = 'id::text'
    provider_credential_models = "provider_credential_id::text || ':' || provider_model_id::text"
    provider_model_capabilities = 'id::text'
    provider_limit_policies = 'id::text'
    model_profiles = 'id::text'
    model_profile_bindings = 'id::text'
    provider_catalog_entries = 'id::text'
    provider_model_capability_presets = 'id::text'
}
$historyTables = [ordered]@{
    provider_call_logs = 'id::text'
    cost_records = 'id::text'
    provider_async_tasks = 'id::text'
    provider_requests = 'id::text'
    provider_test_runs = 'id::text'
    provider_model_capability_attestations = 'id::text'
    provider_model_deletion_tombstones = 'provider_model_id::text'
    provider_model_deletion_render_plan_references = "video_render_plan_id::text || ':' || provider_model_id::text"
}

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
    param(
        [Parameter(Mandatory = $true)][string]$Table,
        [Parameter(Mandatory = $true)][string]$IdentityExpression
    )

    $sql = @"
WITH guarded_rows AS (
    SELECT
        $IdentityExpression AS id,
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

function Get-HistoryFingerprintSet {
    param(
        [Parameter(Mandatory = $true)][string]$Table,
        [Parameter(Mandatory = $true)][string]$IdentityExpression
    )

    $sql = @"
WITH guarded_rows AS (
    SELECT
        $IdentityExpression AS id,
        encode(public.digest(pg_catalog.convert_to(to_jsonb(source_row)::text, 'UTF8'), 'sha256'), 'hex') AS row_hash
    FROM public.$Table AS source_row
)
SELECT json_build_object(
    'rowCount', count(*),
    'rows', COALESCE(
        json_agg(
            json_build_object('id', id, 'hash', row_hash)
            ORDER BY id
        ),
        '[]'::json
    )
)::text
FROM guarded_rows;
"@
    return Invoke-PsqlJson -Sql $sql
}

function Get-Snapshot {
    $configuration = [ordered]@{}
    foreach ($entry in $configurationTables.GetEnumerator()) {
        $configuration[$entry.Key] = Get-ConfigurationFingerprint `
            -Table $entry.Key `
            -IdentityExpression $entry.Value
    }

    $history = [ordered]@{}
    foreach ($entry in $historyTables.GetEnumerator()) {
        $history[$entry.Key] = Get-HistoryFingerprintSet `
            -Table $entry.Key `
            -IdentityExpression $entry.Value
    }

    return [ordered]@{
        schemaVersion = 2
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

function Assert-HistoryFingerprintSubset {
    param(
        [Parameter(Mandatory = $true)][string]$Table,
        [Parameter(Mandatory = $true)]$Expected,
        [Parameter(Mandatory = $true)]$Actual
    )

    $actualRows = [Collections.Generic.Dictionary[string, string]]::new([StringComparer]::Ordinal)
    foreach ($row in @($Actual.rows)) {
        $actualRows.Add([string]$row.id, [string]$row.hash)
    }
    $missing = [Collections.Generic.List[string]]::new()
    $changed = [Collections.Generic.List[string]]::new()
    foreach ($row in @($Expected.rows)) {
        $id = [string]$row.id
        if (-not $actualRows.ContainsKey($id)) {
            $missing.Add($id)
            continue
        }
        if ($actualRows[$id] -cne [string]$row.hash) {
            $changed.Add($id)
        }
    }
    if ($missing.Count -ne 0) {
        $sample = ($missing | Select-Object -First 5) -join ', '
        throw "Provider history table $Table lost $($missing.Count) baseline rows; sample: $sample"
    }
    if ($changed.Count -ne 0) {
        $sample = ($changed | Select-Object -First 5) -join ', '
        throw "Provider history table $Table changed $($changed.Count) baseline rows; sample: $sample"
    }
}

function Test-DrainState {
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
        WHERE status IN ('pending', 'queued', 'running', 'cancelling', 'waiting_review')
    ), '[]'::json),
    'activeWorkflowNodeRuns', COALESCE((
        SELECT json_agg(json_build_object(
            'id', id::text,
            'workflowRunId', workflow_run_id::text,
            'nodeKey', node_key,
            'status', status
        ) ORDER BY created_at)
        FROM workflow_node_runs
        WHERE status IN ('queued', 'running')
    ), '[]'::json),
    'activeProviderRequests', COALESCE((
        SELECT json_agg(json_build_object(
            'id', id::text,
            'taskType', task_type,
            'status', status,
            'createdAt', created_at
        ) ORDER BY created_at)
        FROM provider_requests
        WHERE status IN ('pending', 'running')
    ), '[]'::json),
    'activeProviderCalls', COALESCE((
        SELECT json_agg(json_build_object(
            'id', id::text,
            'taskType', task_type,
            'status', status,
            'createdAt', created_at
        ) ORDER BY created_at)
        FROM provider_call_logs call
        WHERE call.status IN ('queued', 'running')
          AND (
              call.created_at >= now() - make_interval(secs => $StandaloneCallActiveWindowSeconds)
              OR EXISTS (
                  SELECT 1
                  FROM provider_requests request
                  WHERE request.id = call.provider_request_id
                    AND request.status IN ('pending', 'running')
              )
              OR EXISTS (
                  SELECT 1
                  FROM provider_async_tasks task
                  WHERE task.provider_call_id = call.id
                    AND task.status IN ('queued', 'running', 'cancelling')
              )
          )
    ), '[]'::json),
    'activeProviderTasks', COALESCE((
        SELECT json_agg(json_build_object(
            'id', id::text,
            'taskType', task_type,
            'status', status,
            'createdAt', created_at
        ) ORDER BY created_at)
        FROM provider_async_tasks
        WHERE status IN ('queued', 'running', 'cancelling')
    ), '[]'::json),
    'activeProviderLeases', COALESCE((
        SELECT json_agg(json_build_object(
            'id', id::text,
            'taskType', task_type,
            'status', status,
            'expiresAt', expires_at
        ) ORDER BY created_at)
        FROM provider_leases
        WHERE status = 'active'
          AND expires_at > now()
    ), '[]'::json),
    'activeProviderTestRuns', COALESCE((
        SELECT json_agg(json_build_object(
            'id', id::text,
            'testType', test_type,
            'status', status,
            'createdAt', created_at
        ) ORDER BY created_at)
        FROM provider_test_runs
        WHERE status IN ('queued', 'running')
    ), '[]'::json)
)::text;
"@

    $activeWorkflowRuns = @($state.activeWorkflowRuns)
    $activeWorkflowNodeRuns = @($state.activeWorkflowNodeRuns)
    $activeProviderRequests = @($state.activeProviderRequests)
    $activeProviderCalls = @($state.activeProviderCalls)
    $activeProviderTasks = @($state.activeProviderTasks)
    $activeProviderLeases = @($state.activeProviderLeases)
    $activeProviderTestRuns = @($state.activeProviderTestRuns)
    $activeCount = (
        $activeWorkflowRuns.Count +
        $activeWorkflowNodeRuns.Count +
        $activeProviderRequests.Count +
        $activeProviderCalls.Count +
        $activeProviderTasks.Count +
        $activeProviderLeases.Count +
        $activeProviderTestRuns.Count
    )
    if ($activeCount -ne 0) {
        $details = $state | ConvertTo-Json -Depth 8 -Compress
        throw "workflow and Provider runtime is not drained: $details"
    }

    [pscustomobject]@{
        status = 'drained'
        activeWorkflowRuns = 0
        activeWorkflowNodeRuns = 0
        activeProviderRequests = 0
        activeProviderCalls = 0
        activeProviderTasks = 0
        activeProviderLeases = 0
        activeProviderTestRuns = 0
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
        if ([int]$expected.schemaVersion -ne 2) {
            throw "unsupported Provider protection snapshot version: $($expected.schemaVersion)"
        }
        $actual = Get-Snapshot
        foreach ($table in $configurationTables.Keys) {
            Assert-ExactIdentitySet -Table $table -Expected $expected.configuration.$table -Actual $actual.configuration.$table
        }
        foreach ($table in $historyTables.Keys) {
            Assert-HistoryFingerprintSubset -Table $table -Expected $expected.history.$table -Actual $actual.history.$table
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
    'Inspect' {
        $snapshot = Get-Snapshot
        $configurationRowCount = 0
        foreach ($table in $configurationTables.Keys) {
            $configurationRowCount += [long]$snapshot.configuration.$table.rowCount
        }
        $historyRowCount = 0
        foreach ($table in $historyTables.Keys) {
            $historyRowCount += [long]$snapshot.history.$table.rowCount
        }
        [pscustomobject]@{
            status = 'inspected'
            schemaVersion = $snapshot.schemaVersion
            configurationTables = $configurationTables.Count
            configurationRows = $configurationRowCount
            historyTables = $historyTables.Count
            historyRows = $historyRowCount
        } | ConvertTo-Json -Compress
    }
}
