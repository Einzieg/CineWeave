param(
  [switch]$KeepEnvironment
)

$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true

$script = Join-Path $PSScriptRoot 'test-runtime-hardening.ps1'
$arguments = @('-NoProfile', '-File', $script, '-MigrationOnly')
if ($KeepEnvironment) {
  $arguments += '-KeepEnvironment'
}
& pwsh @arguments
if ($LASTEXITCODE -ne 0) {
  throw "Migration test failed with exit code $LASTEXITCODE"
}
