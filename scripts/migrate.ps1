$ErrorActionPreference = "Stop"

$migrationDir = Join-Path $PSScriptRoot "..\db\migrations"
$files = Get-ChildItem -Path $migrationDir -Filter "*.up.sql" | Sort-Object Name

if ($files.Count -eq 0) {
  Write-Host "No migrations found."
  exit 0
}

foreach ($file in $files) {
  Write-Host "Applying $($file.Name)"
  Get-Content -Raw -Path $file.FullName | docker compose exec -T postgres psql -v ON_ERROR_STOP=1 -U cineweave -d cineweave
}

