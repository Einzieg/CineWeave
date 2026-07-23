[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true

$repoRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
$smokeScript = Join-Path $repoRoot 'scripts/smoke-commerce-real-provider.ps1'
$tokens = $null
$parseErrors = $null
[void][System.Management.Automation.Language.Parser]::ParseFile(
  $smokeScript,
  [ref]$tokens,
  [ref]$parseErrors
)
if ($parseErrors.Count -gt 0) {
  $summary = @($parseErrors | ForEach-Object { "$($_.Extent.StartLineNumber): $($_.Message)" }) -join '; '
  throw "Commerce real-provider smoke script has syntax errors: $summary"
}

$source = Get-Content -LiteralPath $smokeScript -Raw -Encoding UTF8
$requiredFragments = @(
  '[switch]$PreflightOnly',
  '[switch]$ConfirmProviderSpend',
  'providerSpendConfirmed = $false',
  'providerSpendConfirmed = $true',
  'providerRequestIds =',
  'providerCallIds =',
  'providerAsyncTaskIds ='
)
foreach ($fragment in $requiredFragments) {
  if (-not $source.Contains($fragment, [StringComparison]::Ordinal)) {
    throw "Commerce real-provider smoke contract is missing: $fragment"
  }
}

$spendGateTriggered = $false
try {
  & $smokeScript `
    -ApiBaseUrl 'http://127.0.0.1:1' `
    -AccessToken 'test-token' `
    -OrganizationId '00000000-0000-4000-8000-000000000001' `
    -ProjectId '00000000-0000-4000-8000-000000000002' `
    -ScriptUnitId '00000000-0000-4000-8000-000000000003'
} catch {
  if ($_.Exception.Message -like '*ConfirmProviderSpend*') {
    $spendGateTriggered = $true
  } else {
    throw
  }
}
if (-not $spendGateTriggered) {
  throw 'Commerce real-provider smoke did not fail closed without -ConfirmProviderSpend.'
}

Write-Host 'Commerce real-provider smoke script contract passed.'
