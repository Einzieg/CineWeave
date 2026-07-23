[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true

$repoRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
$deployScript = Join-Path $repoRoot 'scripts/deploy-commerce-release.ps1'
$tokens = $null
$parseErrors = $null
[void][System.Management.Automation.Language.Parser]::ParseFile(
  $deployScript,
  [ref]$tokens,
  [ref]$parseErrors
)
if ($parseErrors.Count -gt 0) {
  $summary = @($parseErrors | ForEach-Object { "$($_.Extent.StartLineNumber): $($_.Message)" }) -join '; '
  throw "Commerce release deployment script has syntax errors: $summary"
}

$source = Get-Content -LiteralPath $deployScript -Raw -Encoding UTF8
$requiredFragments = @(
  "[ValidateSet('Deploy', 'Smoke', 'Full')]",
  "[string]`$Phase = 'Deploy'",
  '[switch]$ConfirmMainEnvironmentMigration',
  '[switch]$RunPaidSmoke',
  '[switch]$ConfirmProviderSpend',
  'CINEWEAVE_PROVIDER_CONFIGURATION_FROZEN',
  "'DrainCheck'",
  "'Snapshot'",
  "'Verify'",
  "'-PreflightOnly'",
  "'-ConfirmProviderSpend'",
  "'-CheckProviderDrain'",
  "'--profile', 'app', 'up', '-d', '--build'",
  'Wait-RequiredRuntimeServices',
  'Provider configuration writes remain frozen for manual recovery'
)
foreach ($fragment in $requiredFragments) {
  if (-not $source.Contains($fragment, [StringComparison]::Ordinal)) {
    throw "Commerce release deployment contract is missing: $fragment"
  }
}

if ($source -match "'release:check'\s*,\s*'--'") {
  throw 'Commerce release deployment must not pass npm-style -- through pnpm to PowerShell.'
}
if ($source -notmatch 'if \(\$runSmoke\) \{\s+Assert-SmokeConfiguration') {
  throw 'Commerce release deployment must not require a pre-existing Commerce smoke project during Deploy-only releases.'
}

$migrationGateTriggered = $false
try {
  & $deployScript
} catch {
  if ($_.Exception.Message -like '*ConfirmMainEnvironmentMigration*') {
    $migrationGateTriggered = $true
  } else {
    throw
  }
}
if (-not $migrationGateTriggered) {
  throw 'Commerce release deployment did not fail closed without migration confirmation.'
}

$spendGateTriggered = $false
try {
  & $deployScript -Phase Smoke -RunPaidSmoke
} catch {
  if ($_.Exception.Message -like '*ConfirmProviderSpend*') {
    $spendGateTriggered = $true
  } else {
    throw
  }
}
if (-not $spendGateTriggered) {
  throw 'Commerce release deployment did not fail closed without spend confirmation.'
}

Write-Host 'Commerce release deployment script contract passed.'
