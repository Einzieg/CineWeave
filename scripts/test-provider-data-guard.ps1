[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true

$repoRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
$guardScript = Join-Path $repoRoot 'scripts/provider-data-guard.ps1'
$tokens = $null
$parseErrors = $null
[void][System.Management.Automation.Language.Parser]::ParseFile(
  $guardScript,
  [ref]$tokens,
  [ref]$parseErrors
)
if ($parseErrors.Count -gt 0) {
  $summary = @($parseErrors | ForEach-Object { "$($_.Extent.StartLineNumber): $($_.Message)" }) -join '; '
  throw "Provider data guard script has syntax errors: $summary"
}

$source = Get-Content -LiteralPath $guardScript -Raw -Encoding UTF8
$requiredFragments = @(
  'schemaVersion = 2',
  "[ValidateSet('Snapshot', 'Verify', 'DrainCheck', 'Inspect')]",
  'provider_credential_models',
  'provider_model_capability_attestations',
  'provider_model_deletion_tombstones',
  'provider_model_deletion_render_plan_references',
  'provider_requests',
  'provider_test_runs',
  'Assert-HistoryFingerprintSubset',
  "'activeWorkflowNodeRuns'",
  "'activeProviderRequests'",
  "'activeProviderCalls'",
  "'activeProviderTasks'",
  "'activeProviderLeases'",
  "'activeProviderTestRuns'",
  'StandaloneCallActiveWindowSeconds',
  'request.id = call.provider_request_id',
  'task.provider_call_id = call.id'
)
foreach ($fragment in $requiredFragments) {
  if (-not $source.Contains($fragment, [StringComparison]::Ordinal)) {
    throw "Provider data guard contract is missing: $fragment"
  }
}

if ($source.Contains('Get-HistoryIdentitySet', [StringComparison]::Ordinal)) {
  throw 'Provider data guard still uses identity-only history verification.'
}
if ($source.Contains("task_type LIKE 'video.%'", [StringComparison]::Ordinal)) {
  throw 'Provider data guard still limits drain checks to video tasks.'
}

Write-Host 'Provider data guard script contract passed.'
